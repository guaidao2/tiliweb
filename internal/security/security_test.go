package security

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func testPasswordParams() PasswordParams {
	return PasswordParams{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLen: 16, KeyLen: 32}
}

func TestPasswordHashAndVerify(t *testing.T) {
	params := testPasswordParams()
	hash, err := HashPasswordWithParams("correct horse battery staple", params)
	if err != nil {
		t.Fatal(err)
	}
	if hash == "" || hash == "correct horse battery staple" {
		t.Fatal("bad hash")
	}
	ok, err := VerifyPassword("correct horse battery staple", hash)
	if err != nil || !ok {
		t.Fatalf("verify = %v, %v", ok, err)
	}
	ok, err = VerifyPassword("wrong", hash)
	if err != nil || ok {
		t.Fatalf("wrong password accepted: %v, %v", ok, err)
	}
	if PasswordNeedsRehash(hash, params) {
		t.Fatal("hash should match params")
	}
	if !PasswordNeedsRehash(hash, DefaultPasswordParams) {
		t.Fatal("hash should need rehash for stronger params")
	}
}

func TestPasswordUniqueSalts(t *testing.T) {
	params := testPasswordParams()
	a, err := HashPasswordWithParams("same password", params)
	if err != nil {
		t.Fatal(err)
	}
	b, err := HashPasswordWithParams("same password", params)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("hashes should use unique salts")
	}
}

func TestPasswordMalformed(t *testing.T) {
	if _, err := VerifyPassword("x", "not-a-hash"); err == nil {
		t.Fatal("expected malformed hash error")
	}
	if !PasswordNeedsRehash("not-a-hash", testPasswordParams()) {
		t.Fatal("malformed hash should need rehash")
	}
}

func TestTokens(t *testing.T) {
	token, err := NewSessionToken()
	if err != nil || len(token) < 40 {
		t.Fatalf("session token = %q, %v", token, err)
	}
	if HashToken(token) == token {
		t.Fatal("hash should differ from token")
	}
	if HashToken(token) != HashToken(token) {
		t.Fatal("hash should be stable")
	}
	csrf, err := NewCSRFToken()
	if err != nil || len(csrf) != 64 {
		t.Fatalf("csrf token = %q, %v", csrf, err)
	}
	if !ConstantTimeEqual("abc", "abc") || ConstantTimeEqual("abc", "abd") || ConstantTimeEqual("abc", "ab") {
		t.Fatal("constant time equal mismatch")
	}
}

func TestLimiterLockoutAndRate(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	l := NewLimiter(2, time.Minute, 3)
	l.now = func() time.Time { return now }

	if ok, _ := l.Allow("user", "1.1.1.1"); !ok {
		t.Fatal("first attempt should be allowed")
	}
	l.Fail("user", "1.1.1.1")
	l.Fail("user", "1.1.1.1")
	ok, wait := l.Allow("user", "1.1.1.1")
	if ok || wait <= 0 {
		t.Fatalf("expected lockout, ok=%v wait=%v", ok, wait)
	}

	now = now.Add(2 * time.Minute)
	if ok, _ := l.Allow("user", "1.1.1.1"); !ok {
		t.Fatal("lockout should expire")
	}

	// Per-IP failed-attempt rate: 3 failures/min.
	l2 := NewLimiter(0, 0, 2)
	for i := 0; i < 2; i++ {
		if ok, _ := l2.Allow("u"+string(rune('a'+i)), "2.2.2.2"); !ok {
			t.Fatal("rate attempt should be allowed before threshold")
		}
		l2.Fail("u"+string(rune('a'+i)), "2.2.2.2")
	}
	if ok, _ := l2.Allow("u3", "2.2.2.2"); ok {
		t.Fatal("rate limit should block the third attempt")
	}
}

func TestLimiterSuccessClearsLockout(t *testing.T) {
	l := NewLimiter(2, time.Minute, 0)
	l.Fail("user", "1.1.1.1")
	l.Success("user")
	l.Fail("user", "1.1.1.1")
	if ok, _ := l.Allow("user", "1.1.1.1"); !ok {
		t.Fatal("success should reset failure count")
	}
}

func TestLimiterCleanupExpiresIdleFailures(t *testing.T) {
	now := time.Unix(1700000000, 0)
	l := NewLimiter(5, time.Minute, 10)
	l.now = func() time.Time { return now }
	l.Fail("user|alice", "127.0.0.1")
	if len(l.lockouts) != 1 {
		t.Fatalf("lockouts = %d", len(l.lockouts))
	}
	now = now.Add(2 * time.Minute)
	l.Cleanup()
	if len(l.lockouts) != 0 {
		t.Fatalf("idle lockout not cleaned: %+v", l.lockouts)
	}
}

func TestCSRFDoubleSubmit(t *testing.T) {
	csrf := NewCSRF("tili_csrf", "/", false)
	rec := httptest.NewRecorder()
	get := httptest.NewRequest(http.MethodGet, "http://example.com/login", nil)
	token := csrf.Ensure(rec, get)
	if token == "" {
		t.Fatal("empty csrf token")
	}
	cookie := rec.Result().Cookies()
	if len(cookie) != 1 || cookie[0].Name != "tili_csrf" || cookie[0].Value != token {
		t.Fatalf("cookies = %+v", cookie)
	}

	form := url.Values{"_csrf": {token}}
	post := httptest.NewRequest(http.MethodPost, "http://example.com/login", strings.NewReader(form.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.AddCookie(cookie[0])
	if !csrf.Validate(post, "") {
		t.Fatal("valid csrf token rejected")
	}

	bad := httptest.NewRequest(http.MethodPost, "http://example.com/login", strings.NewReader("_csrf=bad"))
	bad.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	bad.AddCookie(cookie[0])
	if csrf.Validate(bad, "") {
		t.Fatal("invalid csrf token accepted")
	}
}

func TestCSRFSessionTokenAndOrigin(t *testing.T) {
	csrf := NewCSRF("tili_csrf", "/", false)
	session := "session-csrf-token"
	form := url.Values{"_csrf": {session}}
	req := httptest.NewRequest(http.MethodPost, "http://example.com/logout", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://example.com")
	if !csrf.Validate(req, session) {
		t.Fatal("session csrf token rejected")
	}

	req.Header.Set("Origin", "http://evil.example")
	if csrf.Validate(req, session) {
		t.Fatal("cross-origin csrf accepted")
	}

	header := httptest.NewRequest(http.MethodPost, "http://example.com/logout", nil)
	header.Header.Set("X-CSRF-Token", session)
	if !csrf.Validate(header, session) {
		t.Fatal("header csrf token rejected")
	}
}
