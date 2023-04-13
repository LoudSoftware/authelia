package model

import (
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

const (
	attestationTypeFIDOU2F = "fido-u2f"
)

// WebAuthnUser is an object to represent a user for the WebAuthn lib.
type WebAuthnUser struct {
	Username    string
	DisplayName string
	Devices     []WebAuthnDevice
}

// HasFIDOU2F returns true if the user has any attestation type `fido-u2f` devices.
func (w WebAuthnUser) HasFIDOU2F() bool {
	for _, c := range w.Devices {
		if c.AttestationType == attestationTypeFIDOU2F {
			return true
		}
	}

	return false
}

// WebAuthnID implements the webauthn.User interface.
func (w WebAuthnUser) WebAuthnID() []byte {
	return []byte(w.Username)
}

// WebAuthnName implements the webauthn.User  interface.
func (w WebAuthnUser) WebAuthnName() string {
	return w.Username
}

// WebAuthnDisplayName implements the webauthn.User interface.
func (w WebAuthnUser) WebAuthnDisplayName() string {
	return w.DisplayName
}

// WebAuthnIcon implements the webauthn.User interface.
func (w WebAuthnUser) WebAuthnIcon() string {
	return ""
}

// WebAuthnCredentials implements the webauthn.User interface.
func (w WebAuthnUser) WebAuthnCredentials() (credentials []webauthn.Credential) {
	credentials = make([]webauthn.Credential, len(w.Devices))

	var credential webauthn.Credential

	for i, device := range w.Devices {
		aaguid, err := device.AAGUID.MarshalBinary()
		if err != nil {
			continue
		}

		credential = webauthn.Credential{
			ID:              device.KID.Bytes(),
			PublicKey:       device.PublicKey,
			AttestationType: device.AttestationType,
			Authenticator: webauthn.Authenticator{
				AAGUID:       aaguid,
				SignCount:    device.SignCount,
				CloneWarning: device.CloneWarning,
			},
		}

		transports := strings.Split(device.Transport, ",")
		credential.Transport = []protocol.AuthenticatorTransport{}

		for _, t := range transports {
			if t == "" {
				continue
			}

			credential.Transport = append(credential.Transport, protocol.AuthenticatorTransport(t))
		}

		credentials[i] = credential
	}

	return credentials
}

// WebAuthnCredentialDescriptors decodes the users credentials into protocol.CredentialDescriptor's.
func (w WebAuthnUser) WebAuthnCredentialDescriptors() (descriptors []protocol.CredentialDescriptor) {
	credentials := w.WebAuthnCredentials()

	descriptors = make([]protocol.CredentialDescriptor, len(credentials))

	for i, credential := range credentials {
		descriptors[i] = credential.Descriptor()
	}

	return descriptors
}

// NewWebAuthnDeviceFromCredential creates a WebAuthnDevice from a webauthn.Credential.
func NewWebAuthnDeviceFromCredential(rpid, username, description string, credential *webauthn.Credential) (device WebAuthnDevice) {
	transport := make([]string, len(credential.Transport))

	for i, t := range credential.Transport {
		transport[i] = string(t)
	}

	device = WebAuthnDevice{
		RPID:            rpid,
		Username:        username,
		CreatedAt:       time.Now(),
		Description:     description,
		KID:             NewBase64(credential.ID),
		PublicKey:       credential.PublicKey,
		AttestationType: credential.AttestationType,
		SignCount:       credential.Authenticator.SignCount,
		CloneWarning:    credential.Authenticator.CloneWarning,
		Transport:       strings.Join(transport, ","),
	}

	aaguid, err := uuid.Parse(hex.EncodeToString(credential.Authenticator.AAGUID))
	if err == nil && aaguid.ID() != 0 {
		device.AAGUID = uuid.NullUUID{Valid: true, UUID: aaguid}
	}

	return device
}

// WebAuthnDevice represents a WebAuthn Device in the database storage.
type WebAuthnDevice struct {
	ID              int           `db:"id"`
	CreatedAt       time.Time     `db:"created_at"`
	LastUsedAt      sql.NullTime  `db:"last_used_at"`
	RPID            string        `db:"rpid"`
	Username        string        `db:"username"`
	Description     string        `db:"description"`
	KID             Base64        `db:"kid"`
	PublicKey       []byte        `db:"public_key"`
	AttestationType string        `db:"attestation_type"`
	Transport       string        `db:"transport"`
	AAGUID          uuid.NullUUID `db:"aaguid"`
	SignCount       uint32        `db:"sign_count"`
	CloneWarning    bool          `db:"clone_warning"`
}

// UpdateSignInInfo adjusts the values of the WebAuthnDevice after a sign in.
func (d *WebAuthnDevice) UpdateSignInInfo(config *webauthn.Config, now time.Time, signCount uint32) {
	d.LastUsedAt = sql.NullTime{Time: now, Valid: true}

	d.SignCount = signCount

	if d.RPID != "" {
		return
	}

	switch d.AttestationType {
	case attestationTypeFIDOU2F:
		d.RPID = config.RPOrigin
	default:
		d.RPID = config.RPID
	}
}

// LastUsed provides LastUsedAt as a *time.Time instead of sql.NullTime.
func (d *WebAuthnDevice) LastUsed() *time.Time {
	if d.LastUsedAt.Valid {
		value := time.Unix(d.LastUsedAt.Time.Unix(), int64(d.LastUsedAt.Time.Nanosecond()))

		return &value
	}

	return nil
}

// ToData converts this WebAuthnDevice into the data format for exporting etc.
func (d *WebAuthnDevice) ToData() WebAuthnDeviceData {
	return WebAuthnDeviceData{
		CreatedAt:       d.CreatedAt,
		LastUsedAt:      d.LastUsed(),
		RPID:            d.RPID,
		Username:        d.Username,
		Description:     d.Description,
		KID:             d.KID.String(),
		PublicKey:       base64.StdEncoding.EncodeToString(d.PublicKey),
		AttestationType: d.AttestationType,
		Transport:       d.Transport,
		AAGUID:          d.AAGUID.UUID.String(),
		SignCount:       d.SignCount,
		CloneWarning:    d.CloneWarning,
	}
}

// MarshalYAML marshals this model into YAML.
func (d *WebAuthnDevice) MarshalYAML() (any, error) {
	return d.ToData(), nil
}

// UnmarshalYAML unmarshalls YAML into this model.
func (d *WebAuthnDevice) UnmarshalYAML(value *yaml.Node) (err error) {
	o := &WebAuthnDeviceData{}

	if err = value.Decode(o); err != nil {
		return err
	}

	if d.PublicKey, err = base64.StdEncoding.DecodeString(o.PublicKey); err != nil {
		return err
	}

	var aaguid uuid.UUID

	if aaguid, err = uuid.Parse(o.AAGUID); err != nil {
		return err
	}

	if aaguid.ID() != 0 {
		d.AAGUID = uuid.NullUUID{Valid: true, UUID: aaguid}
	}

	var kid []byte

	if kid, err = base64.StdEncoding.DecodeString(o.KID); err != nil {
		return err
	}

	d.KID = NewBase64(kid)

	d.CreatedAt = o.CreatedAt
	d.RPID = o.RPID
	d.Username = o.Username
	d.Description = o.Description
	d.AttestationType = o.AttestationType
	d.Transport = o.Transport
	d.SignCount = o.SignCount
	d.CloneWarning = o.CloneWarning

	if o.LastUsedAt != nil {
		d.LastUsedAt = sql.NullTime{Valid: true, Time: *o.LastUsedAt}
	}

	return nil
}

// WebAuthnDeviceData represents a WebAuthn Device in the database storage.
type WebAuthnDeviceData struct {
	CreatedAt       time.Time  `yaml:"created_at"`
	LastUsedAt      *time.Time `yaml:"last_used_at"`
	RPID            string     `yaml:"rpid"`
	Username        string     `yaml:"username"`
	Description     string     `yaml:"description"`
	KID             string     `yaml:"kid"`
	PublicKey       string     `yaml:"public_key"`
	AttestationType string     `yaml:"attestation_type"`
	Transport       string     `yaml:"transport"`
	AAGUID          string     `yaml:"aaguid"`
	SignCount       uint32     `yaml:"sign_count"`
	CloneWarning    bool       `yaml:"clone_warning"`
}

// WebAuthnDeviceExport represents a WebAuthnDevice export file.
type WebAuthnDeviceExport struct {
	WebAuthnDevices []WebAuthnDevice `yaml:"webauthn_devices"`
}

// WebAuthnDeviceDataExport represents a WebAuthnDevice export file.
type WebAuthnDeviceDataExport struct {
	WebAuthnDevices []WebAuthnDeviceData `yaml:"webauthn_devices"`
}

// ToData converts this WebAuthnDeviceExport into a WebAuthnDeviceDataExport.
func (export WebAuthnDeviceExport) ToData() WebAuthnDeviceDataExport {
	data := WebAuthnDeviceDataExport{
		WebAuthnDevices: make([]WebAuthnDeviceData, len(export.WebAuthnDevices)),
	}

	for i, device := range export.WebAuthnDevices {
		data.WebAuthnDevices[i] = device.ToData()
	}

	return data
}

// MarshalYAML marshals this model into YAML.
func (export WebAuthnDeviceExport) MarshalYAML() (any, error) {
	return export.ToData(), nil
}
