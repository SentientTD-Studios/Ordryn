package domain

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"GoTodo/internal/crypto/secret"
	"GoTodo/internal/storage"
)

const (
	auditStreamBatch    = 200
	auditStreamInterval = 15 * time.Second
	auditStreamMaxWait  = 10 * time.Minute
)

var auditStreamClient = &http.Client{Timeout: 15 * time.Second}

// AuditStreamInput patches the audit stream settings. Nil fields stay as they are.
type AuditStreamInput struct {
	Enabled        *bool
	URL            *string
	Secret         *string // "" clears; omitted keeps
	AuthHeader     *string // "" clears; omitted keeps
	IncludeProject *bool
	IncludeTask    *bool
}

// UpdateAuditStream validates and saves the settings. Admins only.
func UpdateAuditStream(in AuditStreamInput) (*storage.AuditStreamSettings, error) {
	s, err := storage.GetAuditStreamSettings()
	if err != nil {
		return nil, err
	}
	if in.URL != nil {
		raw := strings.TrimSpace(*in.URL)
		if raw != "" {
			u, err := url.Parse(raw)
			if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || strings.ContainsAny(raw, "\r\n") {
				return nil, fmt.Errorf("%w: the endpoint must be an https:// URL without credentials", ErrValidation)
			}
		}
		s.URL = raw
	}
	for _, f := range []struct {
		in  *string
		out *string
	}{{in.Secret, &s.SecretEnc}, {in.AuthHeader, &s.HeaderValueEnc}} {
		if f.in == nil {
			continue
		}
		v := strings.TrimSpace(*f.in)
		if v == "" {
			*f.out = ""
			continue
		}
		if strings.ContainsAny(v, "\r\n") {
			return nil, fmt.Errorf("%w: values can't contain line breaks", ErrValidation)
		}
		enc, err := secret.Encrypt(v)
		if err != nil {
			return nil, err
		}
		*f.out = enc
	}
	if in.IncludeProject != nil {
		s.IncludeProject = *in.IncludeProject
	}
	if in.IncludeTask != nil {
		s.IncludeTask = *in.IncludeTask
	}
	if in.Enabled != nil {
		s.Enabled = *in.Enabled
	}
	if s.Enabled && s.URL == "" {
		return nil, fmt.Errorf("%w: set the endpoint URL before turning streaming on", ErrValidation)
	}
	if err := storage.SaveAuditStreamConfig(*s); err != nil {
		return nil, err
	}
	return storage.GetAuditStreamSettings()
}

// SendAuditStreamTest posts one sample event so admins can check the endpoint.
func SendAuditStreamTest(ctx context.Context) error {
	s, err := storage.GetAuditStreamSettings()
	if err != nil {
		return err
	}
	if s.URL == "" {
		return fmt.Errorf("%w: save an endpoint URL first", ErrValidation)
	}
	sample := []storage.AuditStreamEvent{{
		Source: "admin", ID: 0, OccurredAt: time.Now().UTC(), EventType: "audit_stream.test",
		TargetType: "site", Target: "Audit stream test", Metadata: json.RawMessage(`{}`),
	}}
	return postAuditBatch(ctx, s, sample)
}

func postAuditBatch(ctx context.Context, s *storage.AuditStreamSettings, events []storage.AuditStreamEvent) error {
	site := "Ordryn"
	if st, err := storage.GetSiteSettings(); err == nil && strings.TrimSpace(st.SiteName) != "" {
		site = strings.TrimSpace(st.SiteName)
	}
	body, err := json.Marshal(map[string]interface{}{"site": site, "events": events})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Ordryn-Audit-Stream")
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	req.Header.Set("X-Ordryn-Timestamp", ts)
	if s.SecretEnc != "" {
		if key, err := secret.Decrypt(s.SecretEnc); err == nil && key != "" {
			mac := hmac.New(sha256.New, []byte(key))
			mac.Write([]byte(ts + "."))
			mac.Write(body)
			req.Header.Set("X-Ordryn-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
		}
	}
	if s.HeaderValueEnc != "" {
		if v, err := secret.Decrypt(s.HeaderValueEnc); err == nil && v != "" {
			req.Header.Set("Authorization", v)
		}
	}
	resp, err := auditStreamClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("endpoint answered HTTP %d", resp.StatusCode)
	}
	return nil
}

var auditStreamOnce sync.Once

// StartAuditStreamer runs the background loop that forwards new audit events.
// Each tick takes an advisory lock, so several servers never send twice.
func StartAuditStreamer() {
	auditStreamOnce.Do(func() {
		go func() {
			wait := auditStreamInterval
			for {
				time.Sleep(wait)
				sent, err := auditStreamTick(context.Background())
				switch {
				case err != nil:
					storage.RecordAuditStreamError(err.Error())
					wait = min(wait*2, auditStreamMaxWait) // back off while the endpoint is down
				case sent == auditStreamBatch:
					wait = time.Second // more waiting
				default:
					wait = auditStreamInterval
				}
			}
		}()
	})
}

// auditStreamTick sends one batch and returns how many events went out.
func auditStreamTick(ctx context.Context) (int, error) {
	s, err := storage.GetAuditStreamSettings()
	if err != nil || !s.Enabled || s.URL == "" {
		return 0, nil
	}
	ok, release, err := storage.TryAuditStreamLock(ctx)
	if err != nil || !ok {
		return 0, nil
	}
	defer release()
	events, err := storage.NextAuditStreamBatch(s, auditStreamBatch)
	if err != nil || len(events) == 0 {
		return 0, err
	}
	if err := postAuditBatch(ctx, s, events); err != nil {
		return 0, err
	}
	return len(events), storage.AdvanceAuditStream(events)
}
