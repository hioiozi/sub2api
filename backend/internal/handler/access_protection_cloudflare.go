package handler

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
)

type accessRule map[string]any
type accessPending struct {
	Before  accessRule       `json:"before"`
	After   accessRule       `json:"after"`
	Records []AccessIPRecord `json:"records"`
}
type accessState struct {
	Version  int              `json:"version"`
	BaseRule accessRule       `json:"base_rule,omitempty"`
	LastRule accessRule       `json:"last_rule,omitempty"`
	Records  []AccessIPRecord `json:"records"`
	Pending  *accessPending   `json:"pending,omitempty"`
}

func (h *AccessProtectionHandler) stateFile() string { return filepath.Join(h.dir, "state.json") }
func (h *AccessProtectionHandler) load() (accessState, error) {
	s := accessState{Version: 2, Records: []AccessIPRecord{}}
	b, err := os.ReadFile(h.stateFile())
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err = json.Unmarshal(b, &s); err != nil {
		return s, err
	}
	if s.Version == 1 && len(s.Records) == 0 {
		s.Version = 2
	} // Only empty prototype states migrate automatically.
	if s.Version != 2 || (len(s.Records) > 0 && s.BaseRule == nil) {
		return s, errors.New("access state requires administrator migration")
	}
	return s, nil
}
func (h *AccessProtectionHandler) save(s accessState) error {
	if err := os.MkdirAll(h.dir, 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(h.dir, "state-*.tmp")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(name, h.stateFile()); err != nil {
		return err
	}
	// Linux deployment: flush the directory entry as well as the journal bytes.
	if runtime.GOOS != "windows" {
		d, e := os.Open(h.dir)
		if e != nil {
			return e
		}
		defer d.Close()
		return d.Sync()
	}
	return nil
}
func (h *AccessProtectionHandler) ready(ctx context.Context) (accessState, error) {
	s, err := h.load()
	if err == nil && s.Pending != nil {
		err = h.recover(ctx, &s)
	}
	return s, err
}
func accessExpression(base string, records []AccessIPRecord) (string, error) {
	unique := map[string]bool{}
	for _, r := range records {
		ip, err := validPublicIP(r.IP)
		if err != nil {
			return "", err
		}
		unique[ip] = true
	}
	if len(unique) == 0 {
		return base, nil
	}
	ips := make([]string, 0, len(unique))
	for ip := range unique {
		ips = append(ips, ip)
	}
	sort.Strings(ips)
	expr := "(" + base + ") and not ip.src in {" + strings.Join(ips, " ") + "}"
	if len(expr) > 4000 {
		return "", errors.New("rule capacity reached")
	}
	return expr, nil
}
func cloneAccessRule(r accessRule) accessRule {
	out := accessRule{}
	for k, v := range r {
		out[k] = v
	}
	return out
}
func (h *AccessProtectionHandler) commit(ctx context.Context, s *accessState, records []AccessIPRecord) error {
	remote, err := h.rule(ctx)
	if err != nil {
		return err
	}
	if s.BaseRule == nil {
		if h.expectedBase == "" || remote["expression"] != h.expectedBase || remote["action"] != "block" || remote["enabled"] != true {
			return errors.New("configured base rule mismatch")
		}
		s.BaseRule = cloneAccessRule(remote)
		s.LastRule = cloneAccessRule(remote)
	}
	if !reflect.DeepEqual(remote, s.LastRule) {
		return errors.New("Cloudflare rule changed externally; operator review required")
	}
	expr, err := accessExpression(s.BaseRule["expression"].(string), records)
	if err != nil {
		return err
	}
	after := cloneAccessRule(s.BaseRule)
	after["expression"] = expr
	s.Pending = &accessPending{Before: remote, After: after, Records: records}
	// Durable intent MUST precede external side effects.
	if err = h.save(*s); err != nil {
		return err
	}
	return h.recover(ctx, s)
}
func (h *AccessProtectionHandler) recover(ctx context.Context, s *accessState) error {
	p := s.Pending
	if p == nil {
		return nil
	}
	remote, err := h.rule(ctx)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(remote, p.After) {
		if !reflect.DeepEqual(remote, p.Before) {
			return errors.New("pending rule drift; administrator review required")
		}
		err = h.cf(ctx, http.MethodPatch, h.rulePath(), p.After, nil)
		// Read back even on failure: a timeout may have occurred AFTER applying.
		actual, readErr := h.rule(ctx)
		if readErr != nil {
			return readErr
		}
		if !reflect.DeepEqual(actual, p.After) {
			var ce *accessCFError
			if errors.As(err, &ce) && ce.Status >= 400 && ce.Status < 500 && ce.Status != 408 && ce.Status != 429 && reflect.DeepEqual(actual, p.Before) {
				s.Pending = nil
				if e := h.save(*s); e != nil {
					return e
				} // Definitive rejection, no remote side effect.
			}
			if err != nil {
				return err
			}
			return errors.New("Cloudflare read-back differs from intended rule")
		}
	}
	s.LastRule = p.After
	s.Records = p.Records
	s.Pending = nil
	return h.save(*s)
}
func (h *AccessProtectionHandler) token() (string, error) {
	keyBlob, err := os.ReadFile(h.keyFile)
	if err != nil {
		return "", errors.New("Cloudflare key file unavailable")
	}
	key, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(string(keyBlob)))
	if err != nil || len(key) != 32 {
		return "", errors.New("Cloudflare key invalid")
	}
	tokenFile := h.rotatedTokenFile()
	if _, statErr := os.Stat(tokenFile); errors.Is(statErr, os.ErrNotExist) {
		tokenFile = h.tokenFile
	} else if statErr != nil {
		return "", errors.New("Cloudflare token file unavailable")
	}
	blob, err := os.ReadFile(tokenFile)
	if err != nil {
		return "", errors.New("Cloudflare token file unavailable")
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(blob)))
	if err != nil {
		return "", errors.New("Cloudflare ciphertext invalid")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(data) < gcm.NonceSize()+gcm.Overhead() {
		return "", errors.New("Cloudflare ciphertext truncated")
	}
	plain, err := gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], nil)
	if err != nil {
		return "", errors.New("Cloudflare token decryption failed")
	}
	return string(plain), nil
}

func (h *AccessProtectionHandler) rotatedTokenFile() string {
	return filepath.Join(h.dir, "token.enc")
}

func (h *AccessProtectionHandler) encryptToken(token string) ([]byte, error) {
	keyBlob, err := os.ReadFile(h.keyFile)
	if err != nil {
		return nil, errors.New("Cloudflare key file unavailable")
	}
	key, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(string(keyBlob)))
	if err != nil || len(key) != 32 {
		return nil, errors.New("Cloudflare key invalid")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	return []byte(base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(token), nil))), nil
}

func (h *AccessProtectionHandler) storeToken(token string) error {
	blob, err := h.encryptToken(token)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(h.dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(h.dir, "token-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(blob)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(f.Name(), h.rotatedTokenFile()); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		d, e := os.Open(h.dir)
		if e != nil {
			return e
		}
		defer d.Close()
		return d.Sync()
	}
	return nil
}

type accessCFError struct{ Status int }

func (e *accessCFError) Error() string { return fmt.Sprintf("Cloudflare HTTP %d", e.Status) }
func (h *AccessProtectionHandler) rulesetPath() string {
	return fmt.Sprintf("/zones/%s/rulesets/%s", h.zoneID, h.rulesetID)
}
func (h *AccessProtectionHandler) rulePath() string { return h.rulesetPath() + "/rules/" + h.ruleID }
func (h *AccessProtectionHandler) cf(ctx context.Context, method, path string, body, out any) error {
	token, err := h.token()
	if err != nil {
		return err
	}
	return h.cfWithToken(ctx, token, method, path, body, out)
}
func (h *AccessProtectionHandler) cfWithToken(ctx context.Context, token, method, path string, body, out any) error {
	var payload io.Reader
	if body != nil {
		b, e := json.Marshal(body)
		if e != nil {
			return e
		}
		payload = strings.NewReader(string(b))
	}
	req, err := http.NewRequestWithContext(ctx, method, h.apiURL+path, payload)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, err := h.client.Do(req)
	if err != nil {
		return errors.New("Cloudflare request interrupted")
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if err != nil {
		return err
	}
	var envelope struct {
		Success bool            `json:"success"`
		Result  json.RawMessage `json:"result"`
	}
	if json.Unmarshal(raw, &envelope) != nil || !envelope.Success || res.StatusCode >= 300 {
		return &accessCFError{res.StatusCode}
	}
	if out != nil {
		return json.Unmarshal(envelope.Result, out)
	}
	return nil
}
func (h *AccessProtectionHandler) rule(ctx context.Context) (accessRule, error) {
	return h.ruleWithToken(ctx, "")
}
func (h *AccessProtectionHandler) ruleWithToken(ctx context.Context, token string) (accessRule, error) {
	if h.zoneID == "" || h.rulesetID == "" || h.ruleID == "" {
		return nil, errors.New("Cloudflare identifiers missing")
	}
	var rules struct {
		Rules []accessRule `json:"rules"`
	}
	var err error
	if token == "" {
		err = h.cf(ctx, http.MethodGet, h.rulesetPath(), nil, &rules)
	} else {
		err = h.cfWithToken(ctx, token, http.MethodGet, h.rulesetPath(), nil, &rules)
	}
	if err != nil {
		return nil, err
	}
	for _, r := range rules.Rules {
		if r["id"] == h.ruleID {
			// PATCH replaces the rule definition. Keep all writable fields, stripping
			// only documented response metadata; never change action/description/ref.
			delete(r, "id")
			delete(r, "version")
			delete(r, "last_updated")
			return r, nil
		}
	}
	return nil, errors.New("Cloudflare rule missing")
}
