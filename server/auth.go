package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

const (
	jwtKeyFilename = "jwt.key"
	authPassKey    = "auth.password_hash"
	jwtTTL         = 12 * time.Hour
	// setupTokenFilename is the one-time first-run setup token, written into the
	// data dir (mode 0600) when no admin password exists yet. POST /api/auth/init
	// must present it as JSON field "setup_token"; it is deleted after success.
	setupTokenFilename = "setup-token"
	keyChars           = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

	// The lower bound matches the setup page's frontend check -- validating only in the frontend is no validation, since
	// hitting the API directly bypasses it. The upper bound is bcrypt's hard limit: past 72 bytes GenerateFromPassword returns
	// ErrPasswordTooLong; rejecting early beats handing the user a cryptic "password hashing failed".
	minPasswordRunes = 8
	maxPasswordBytes = 72
)

// errDataSourceUnavailable is the uniform reply when a password-related read fails. These handlers must never
// treat "couldn't read" as "not set": authInit once let requests through on a database error this way, allowing an
// unauthenticated request to overwrite the existing admin password.
const errDataSourceUnavailable = "Data source temporarily unavailable, please try again later"

// validatePassword returns an empty string on success, otherwise a reason that can be shown to the user directly.
func validatePassword(pw string) string {
	if utf8.RuneCountInString(pw) < minPasswordRunes {
		return fmt.Sprintf("Password must be at least %d characters", minPasswordRunes)
	}
	if len(pw) > maxPasswordBytes {
		return fmt.Sprintf("Password must not exceed %d bytes", maxPasswordBytes)
	}
	return ""
}

// loadOrCreateJWTKey reads the 32-byte signing key from keyDir/jwt.key. keyDir is
// the project base dir (next to the executable), NOT the browsable workspace root
// (dataDir) — the signing key must never be listable/downloadable via the file
// manager. Legacy installs kept it at dataDir/jwt.key; if present there and not yet
// at the new location, it is migrated (key preserved, so sessions stay valid) and
// the old file removed so it disappears from the workspace. On first run a random
// key is generated and persisted.
func loadOrCreateJWTKey(keyDir, dataDir string) ([]byte, error) {
	path := filepath.Join(keyDir, jwtKeyFilename)
	// one-time migration out of the old in-workspace location.
	if legacy := filepath.Join(dataDir, jwtKeyFilename); legacy != path {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			if data, rerr := os.ReadFile(legacy); rerr == nil {
				if werr := os.WriteFile(path, data, 0o600); werr == nil {
					_ = os.Remove(legacy)
					log.Printf("[auth] JWT key migrated from %s to %s (moved out of the browsable workspace)", legacy, path)
				}
			}
		}
	}
	if data, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(data))) >= 32 {
		return []byte(strings.TrimSpace(string(data))), nil
	}
	buf, err := generateJWTKey()
	if err != nil {
		return nil, err
	}
	if err := writeFileAtomic(path, buf, 0o600); err != nil {
		return nil, fmt.Errorf("write jwt key: %w", err)
	}
	log.Printf("[auth] new JWT key written to %s", path)
	return buf, nil
}

// generateJWTKey returns a fresh random 32-char signing key.
func generateJWTKey() ([]byte, error) {
	buf := make([]byte, 32)
	for i := range buf {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(keyChars))))
		if err != nil {
			return nil, fmt.Errorf("generate jwt key: %w", err)
		}
		buf[i] = keyChars[n.Int64()]
	}
	return buf, nil
}

// writeFileAtomic writes data to a temp file in the same directory with the given
// mode and renames it over path, so a crash never leaves a truncated file.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// signingKey returns the current JWT signing key (it is rotated on password change).
func (s *Server) signingKey() []byte {
	s.authMu.RLock()
	defer s.authMu.RUnlock()
	return s.jwtKey
}

// rotateJWTKey replaces the signing key with a fresh random one, persisting it to
// keyDir/jwt.key when keyDir is set. Every previously issued token becomes invalid.
func (s *Server) rotateJWTKey() error {
	key, err := generateJWTKey()
	if err != nil {
		return err
	}
	s.authMu.Lock()
	defer s.authMu.Unlock()
	if s.keyDir != "" {
		if err := writeFileAtomic(filepath.Join(s.keyDir, jwtKeyFilename), key, 0o600); err != nil {
			return fmt.Errorf("write jwt key: %w", err)
		}
	}
	s.jwtKey = key
	return nil
}

// initSetupToken prepares the one-time first-run setup token. When no admin
// password is configured (or the database cannot confirm one is), a random token
// is generated with crypto/rand, written to <dir>/setup-token (0600) and logged.
// When a password already exists, any stale token file is removed.
func (s *Server) initSetupToken(dir string) {
	path := filepath.Join(dir, setupTokenFilename)
	if s.m != nil && s.m.pg != nil {
		if hash, _, err := s.m.pg.GetSetting(authPassKey); err == nil && hash != "" {
			_ = os.Remove(path)
			return
		}
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		log.Fatalf("[auth] generate setup token: %v", err)
	}
	tok := hex.EncodeToString(raw)
	if err := writeFileAtomic(path, []byte(tok+"\n"), 0o600); err != nil {
		log.Printf("[auth] WARNING: could not write setup token file %s: %v", path, err)
	}
	s.authMu.Lock()
	s.setupToken = tok
	s.setupTokenPath = path
	s.authMu.Unlock()
	log.Printf("[auth] No admin password is set. One-time setup token (also in %s): %s", path, tok)
}

// checkSetupToken reports whether tok matches the pending setup token
// (constant-time). It is false when no setup token is pending.
func (s *Server) checkSetupToken(tok string) bool {
	s.authMu.RLock()
	want := s.setupToken
	s.authMu.RUnlock()
	if want == "" || tok == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(strings.TrimSpace(tok)), []byte(want)) == 1
}

// consumeSetupToken invalidates the setup token after a successful setup.
func (s *Server) consumeSetupToken() {
	s.authMu.Lock()
	path := s.setupTokenPath
	s.setupToken = ""
	s.setupTokenPath = ""
	s.authMu.Unlock()
	if path != "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			log.Printf("[auth] WARNING: could not remove setup token file %s: %v", path, err)
		}
	}
}

// signJWT issues a 12-hour HS256 token for user Elenxa.
func signJWT(key []byte) (string, error) {
	return jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject:   "Elenxa",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(jwtTTL)),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
	}).SignedString(key)
}

// verifyJWT returns true when tokenStr is a valid, non-expired HS256 token.
func verifyJWT(tokenStr string, key []byte) bool {
	if tokenStr == "" || len(key) == 0 {
		return false
	}
	t, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return key, nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
	return err == nil && t.Valid
}

// extractToken reads the JWT from the Authorization: Bearer header. For safe,
// read-only requests (GET/HEAD) it additionally accepts the elenxa_token cookie
// (used by a few plain fetches in the UI; the cookie must be SameSite=Strict) and
// the ?token= query param (EventSource/SSE cannot set headers). State-changing
// requests MUST carry the Bearer header, so a cross-site form/fetch can never ride
// on an ambient cookie (CSRF).
func extractToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return ""
	}
	if c, err := r.Cookie("elenxa_token"); err == nil && c.Value != "" {
		return c.Value
	}
	return r.URL.Query().Get("token")
}

// requireAuth wraps h with JWT validation.
// /api/auth/* and /api/health are exempt.
func (s *Server) requireAuth(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if strings.HasPrefix(p, "/api/auth/") || p == "/api/health" {
			h.ServeHTTP(w, r)
			return
		}
		tok := extractToken(r)
		if tok == "" {
			writeErr(w, 401, "Unauthorized")
			return
		}
		if !verifyJWT(tok, s.signingKey()) {
			writeErr(w, 401, "Token is invalid or expired")
			return
		}
		h.ServeHTTP(w, r)
	})
}

// GET /api/auth/status — reports whether the admin password has been initialised.
// A read failure must return 503, not initialized:false: on initialized:false the frontend sends the user
// to /setup to set a password (login/page.tsx), so dressing a database fault up as a 200 pushes the user
// toward overwriting the existing password.
func (s *Server) authStatus(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	hash, _, err := pg.GetSetting(authPassKey)
	if err != nil {
		writeErr(w, 503, errDataSourceUnavailable)
		return
	}
	writeJSON(w, 200, map[string]any{"initialized": hash != ""})
}

// POST /api/auth/init — sets the password for the first time; rejected if already set.
// Body: {"password": "...", "setup_token": "<contents of <datadir>/setup-token>"}.
// The setup token closes the first-run race where whoever reached this endpoint
// first on a fresh install became admin.
func (s *Server) authInit(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	existing, _, err := pg.GetSetting(authPassKey)
	if err != nil {
		writeErr(w, 503, errDataSourceUnavailable)
		return
	}
	if existing != "" {
		writeErr(w, 403, "Password is already set")
		return
	}
	var req struct {
		Password   string `json:"password"`
		SetupToken string `json:"setup_token"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "Malformed request")
		return
	}
	ip := clientIP(r)
	if ok, retry := s.loginLim.allow(ip, time.Now()); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
		writeErr(w, 429, "Too many attempts, please try again later")
		return
	}
	if !s.checkSetupToken(req.SetupToken) {
		s.loginLim.fail(ip, time.Now())
		time.Sleep(authFailDelay)
		writeErr(w, 403, "Invalid setup token: use the one-time token from the server log or from the setup-token file in the key directory (ELENXA_KEY_DIR, Docker: /app/keys)")
		return
	}
	if req.Password == "" {
		writeErr(w, 400, "Password cannot be empty")
		return
	}
	if msg := validatePassword(req.Password); msg != "" {
		writeErr(w, 400, msg)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeErr(w, 500, "Failed to hash password")
		return
	}
	// Use INSERT ... ON CONFLICT DO NOTHING rather than upsert: the GetSetting above is only a
	// fail-fast path; the real "settable only once" guarantee lives in the primary-key constraint. bcrypt takes tens of ms,
	// during which another request could well set the password first, and the read check itself can fail due to faults.
	inserted, err := pg.InsertSettingIfAbsent(authPassKey, string(hash))
	if err != nil {
		writeErr(w, 500, "Save failed: "+err.Error())
		return
	}
	if !inserted {
		writeErr(w, 403, "Password is already set")
		return
	}
	s.consumeSetupToken()
	tok, err := signJWT(s.signingKey())
	if err != nil {
		writeErr(w, 500, "Failed to generate token")
		return
	}
	writeJSON(w, 200, map[string]any{"token": tok})
}

// POST /api/auth/change-password — changes the admin password. Requires a valid
// token (this route is under /api/auth/* which requireAuth exempts, so the token
// is validated here) AND the current password.
func (s *Server) authChangePassword(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	if !verifyJWT(extractToken(r), s.signingKey()) {
		writeErr(w, 401, "Unauthorized")
		return
	}
	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "Malformed request")
		return
	}
	if req.NewPassword == "" {
		writeErr(w, 400, "New password cannot be empty")
		return
	}
	if msg := validatePassword(req.NewPassword); msg != "" {
		writeErr(w, 400, msg)
		return
	}
	hash, ok, err := pg.GetSetting(authPassKey)
	if err != nil {
		writeErr(w, 503, errDataSourceUnavailable)
		return
	}
	if !ok || hash == "" {
		writeErr(w, 403, "Password not initialized, please set a password first")
		return
	}
	ip := clientIP(r)
	if ok, retry := s.loginLim.allow(ip, time.Now()); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
		writeErr(w, 429, "Too many attempts, please try again later")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.OldPassword)); err != nil {
		s.loginLim.fail(ip, time.Now())
		time.Sleep(authFailDelay)
		writeErr(w, 401, "Current password is incorrect")
		return
	}
	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		writeErr(w, 500, "Failed to hash password")
		return
	}
	if err := pg.SetSetting(authPassKey, string(newHash)); err != nil {
		writeErr(w, 500, "Save failed: "+err.Error())
		return
	}
	// Invalidate every existing session by rotating the signing key, then hand the
	// caller a fresh token so the session that changed the password stays logged in.
	if err := s.rotateJWTKey(); err != nil {
		log.Printf("[auth] rotate JWT key after password change: %v", err)
		writeErr(w, 500, "Password changed, but session key rotation failed; please restart the service")
		return
	}
	tok, err := signJWT(s.signingKey())
	if err != nil {
		writeErr(w, 500, "Failed to generate token")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "token": tok})
}

// POST /api/auth/login — validates username/password and returns a JWT.
func (s *Server) authLogin(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "Malformed request")
		return
	}
	ip := clientIP(r)
	if ok, retry := s.loginLim.allow(ip, time.Now()); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
		writeErr(w, 429, "Too many failed logins, please try again later")
		return
	}
	if req.Username != "Elenxa" {
		s.loginLim.fail(ip, time.Now())
		time.Sleep(authFailDelay)
		writeErr(w, 401, "Incorrect username or password")
		return
	}
	hash, ok, err := pg.GetSetting(authPassKey)
	if err != nil {
		writeErr(w, 503, errDataSourceUnavailable)
		return
	}
	if !ok || hash == "" {
		writeErr(w, 403, "Password not initialized, please set a password first")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)); err != nil {
		s.loginLim.fail(ip, time.Now())
		time.Sleep(authFailDelay)
		writeErr(w, 401, "Incorrect username or password")
		return
	}
	s.loginLim.success(ip)
	tok, err := signJWT(s.signingKey())
	if err != nil {
		writeErr(w, 500, "Failed to generate token")
		return
	}
	writeJSON(w, 200, map[string]any{"token": tok})
}

// --- login rate limiting (in-memory, stdlib only) ---

const (
	authFailDelay     = 750 * time.Millisecond // fixed delay added to every failed attempt
	loginWindow       = 5 * time.Minute        // failure counting window
	loginMaxPerIP     = 5                      // failures per IP per window before lockout
	loginLockout      = 5 * time.Minute        // per-IP lockout after loginMaxPerIP failures
	loginMaxGlobal    = 30                     // failures across all IPs per window before global lockout
	loginMaxTrackedIP = 10000                  // cap on tracked IPs (expired entries pruned first)
)

type failWindow struct {
	start       time.Time
	count       int
	lockedUntil time.Time
}

// loginLimiter tracks failed authentication attempts per client IP and globally.
// The zero value is ready to use.
type loginLimiter struct {
	mu     sync.Mutex
	perIP  map[string]*failWindow
	global failWindow
}

// allow reports whether ip may attempt authentication now; when not, it returns
// how long until the lockout lifts.
func (l *loginLimiter) allow(ip string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Before(l.global.lockedUntil) {
		return false, l.global.lockedUntil.Sub(now)
	}
	if fw := l.perIP[ip]; fw != nil && now.Before(fw.lockedUntil) {
		return false, fw.lockedUntil.Sub(now)
	}
	return true, 0
}

// fail records one failed attempt for ip.
func (l *loginLimiter) fail(ip string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.perIP == nil {
		l.perIP = map[string]*failWindow{}
	}
	fw := l.perIP[ip]
	if fw == nil {
		if len(l.perIP) >= loginMaxTrackedIP {
			l.pruneLocked(now)
		}
		fw = &failWindow{}
		l.perIP[ip] = fw
	}
	bump(fw, now, loginMaxPerIP, loginLockout)
	if bump(&l.global, now, loginMaxGlobal, loginWindow) {
		log.Printf("[auth] global login lockout: %d failed attempts within %s", loginMaxGlobal, loginWindow)
	}
}

// success clears the failure history of ip.
func (l *loginLimiter) success(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.perIP, ip)
}

// bump counts one failure in fw and locks it for lockout once limit is reached.
// Returns true when this failure triggered the lock.
func bump(fw *failWindow, now time.Time, limit int, lockout time.Duration) bool {
	if fw.start.IsZero() || now.Sub(fw.start) > loginWindow {
		fw.start = now
		fw.count = 0
	}
	fw.count++
	if fw.count >= limit {
		fw.lockedUntil = now.Add(lockout)
		fw.start = time.Time{}
		fw.count = 0
		return true
	}
	return false
}

func (l *loginLimiter) pruneLocked(now time.Time) {
	for ip, fw := range l.perIP {
		if now.After(fw.lockedUntil) && (fw.start.IsZero() || now.Sub(fw.start) > loginWindow) {
			delete(l.perIP, ip)
		}
	}
	if len(l.perIP) >= loginMaxTrackedIP {
		// Still full (an address spray): drop everything rather than grow unbounded.
		// The global limiter keeps bounding the total attempt rate.
		l.perIP = map[string]*failWindow{}
	}
}

// clientIP is the TCP peer address. X-Forwarded-For is deliberately ignored: it is
// client-controlled and would let an attacker rotate "IPs" freely.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
