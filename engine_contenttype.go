package main

import (
	"fmt"
	"log"

	"github.com/godbus/dbus/v5"
)

// IBus input-purpose constants mirror GtkInputPurpose (ibus/enums.h).
const (
	IBusInputPurposeNormal   uint32 = 0
	IBusInputPurposeAlpha    uint32 = 1
	IBusInputPurposeDigits   uint32 = 2
	IBusInputPurposeNumber   uint32 = 3
	IBusInputPurposePhone    uint32 = 4
	IBusInputPurposeUrl      uint32 = 5
	IBusInputPurposeEmail    uint32 = 6
	IBusInputPurposePassword uint32 = 7
	IBusInputPurposePin      uint32 = 8
	IBusInputPurposeTerminal uint32 = 10
)

// isPasswordField reports whether the focused field is password/PIN, in which
// case all Vietnamese processing must be skipped.
func (e *IBusBambooEngine) isPasswordField() bool {
	e.Lock()
	defer e.Unlock()
	return e.contentPurpose == IBusInputPurposePassword || e.contentPurpose == IBusInputPurposePin
}

// isUrlField reports whether the active input context specifies IBusInputPurposeUrl,
// where inline auto-completion selections may intercept forwarded backspace events.
func (e *IBusBambooEngine) isUrlField() bool {
	e.Lock()
	defer e.Unlock()
	return e.contentPurpose == IBusInputPurposeUrl
}

// setContentType is the shared handler for Properties.Set and the legacy
// SetContentType method.
func (e *IBusBambooEngine) setContentType(purpose, hints uint32) {
	e.Lock()
	wasPassword := e.contentPurpose == IBusInputPurposePassword || e.contentPurpose == IBusInputPurposePin
	e.contentPurpose = purpose
	e.contentHints = hints
	isPassword := e.contentPurpose == IBusInputPurposePassword || e.contentPurpose == IBusInputPurposePin
	e.Unlock()

	// Log the numeric purpose/hints only — never field content.
	log.Printf(
		"SetContentType: purpose=%d hints=%d password=%v",
		purpose,
		hints,
		isPassword,
	)
	if isPassword && !wasPassword {
		e.resetForPasswordField()
	}
}

// resetForPasswordField discards composition and invalidates queued keys.
// Do not use resetBuffer: it could commit the preedit into the secret field.
func (e *IBusBambooEngine) resetForPasswordField() {
	e.invalidatePendingKeys()
	log.Print("Password field: passthrough ON (drained pending keys reset preeditor/shadow; no rewrite; no content log)")
}

// decodeContentType decodes the (uu) dbus variant as []interface{}{purpose, hints}.
func decodeContentType(value dbus.Variant) (uint32, uint32, bool) {
	raw := value.Value()
	vals, ok := raw.([]interface{})
	if !ok || len(vals) != 2 {
		return 0, 0, false
	}
	p, ok1 := vals[0].(uint32)
	h, ok2 := vals[1].(uint32)
	if !ok1 || !ok2 {
		return 0, 0, false
	}
	return p, h, true
}

// Set implements org.freedesktop.DBus.Properties.Set (ContentType only).
// @method(in_signature="ssv")
func (e *IBusBambooEngine) Set(iface string, prop string, value dbus.Variant) *dbus.Error {
	if prop != "ContentType" {
		return nil
	}
	purpose, hints, ok := decodeContentType(value)
	if !ok {
		return dbus.MakeFailedError(fmt.Errorf("ContentType: unexpected variant %#v", value.Value()))
	}
	e.setContentType(purpose, hints)
	log.Print("[via Properties.Set]")
	return nil
}

// GetAll implements org.freedesktop.DBus.Properties.GetAll.
// @method(in_signature="s", out_signature="a{sv}")
func (e *IBusBambooEngine) GetAll(iface string) (map[string]dbus.Variant, *dbus.Error) {
	if iface != "org.freedesktop.IBus.Engine" {
		return map[string]dbus.Variant{}, nil
	}
	e.Lock()
	p, h := e.contentPurpose, e.contentHints
	e.Unlock()
	return map[string]dbus.Variant{
		"ContentType": dbus.MakeVariant([]interface{}{p, h}),
	}, nil
}

// Get implements org.freedesktop.DBus.Properties.Get (ContentType only).
// @method(in_signature="ss", out_signature="v")
func (e *IBusBambooEngine) Get(iface string, prop string) (dbus.Variant, *dbus.Error) {
	if prop != "ContentType" {
		return dbus.Variant{}, dbus.MakeFailedError(fmt.Errorf("property %q not supported", prop))
	}
	e.Lock()
	p, h := e.contentPurpose, e.contentHints
	e.Unlock()
	return dbus.MakeVariant([]interface{}{p, h}), nil
}
