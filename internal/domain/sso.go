package domain

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"GoTodo/internal/crypto/secret"
	"GoTodo/internal/storage"

	"golang.org/x/crypto/bcrypt"
)

// SSOClaims is what the provider said about the person signing in, taken from a
// verified ID token.
type SSOClaims struct {
	Issuer            string
	Subject           string
	Email             string
	EmailVerified     bool
	GivenName         string
	FamilyName        string
	Name              string
	PreferredUsername string
	Groups            []string
}

// SSOLoginResult is the account an SSO sign-in resolved to.
type SSOLoginResult struct {
	UserID  int
	Created bool
	Linked  bool
}

// ErrSSODenied is a sign-in the site refuses; its message is safe to show the person.
var ErrSSODenied = errors.New("sso denied")

func ssoDenied(msg string) error { return fmt.Errorf("%w: %s", ErrSSODenied, msg) }

// ResolveSSOLogin finds or creates the account for a verified ID token:
//  1. an account already linked to (issuer, subject);
//  2. otherwise an account with the same email, when the provider verified it;
//  3. otherwise a new account, when the settings allow it.
//
// It then applies the group → team mapping in company mode.
func ResolveSSOLogin(s *storage.SSOSettings, c SSOClaims) (*SSOLoginResult, error) {
	if s == nil || !s.Configured() {
		return nil, ssoDenied("single sign-on is not set up on this site")
	}
	c.Email = strings.TrimSpace(c.Email)
	if c.Issuer == "" || c.Subject == "" {
		return nil, ssoDenied("the identity provider didn't say who you are")
	}

	res := &SSOLoginResult{}
	userID, err := storage.FindUserByIdentity(c.Issuer, c.Subject)
	if err != nil {
		return nil, err
	}
	if userID == 0 {
		if c.Email == "" || !c.EmailVerified {
			return nil, ssoDenied("your identity provider didn't share a verified email address")
		}
		if !s.DomainAllowed(c.Email) {
			return nil, ssoDenied("accounts with that email domain can't sign in here")
		}
		if u, err := storage.GetUserByEmail(c.Email); err == nil && u != nil {
			userID = u.ID
			res.Linked = true
		} else if !s.AutoCreate {
			return nil, ssoDenied("there's no account for " + c.Email + " on this site. Ask an admin to create one")
		} else {
			userID, err = createSSOAccount(c)
			if err != nil {
				return nil, err
			}
			res.Created = true
		}
	}

	if storage.IsAgentUser(userID) {
		return nil, ssoDenied("agent accounts can't sign in")
	}
	if storage.IsUserBannedByID(userID) {
		return nil, ssoDenied("this account has been deactivated. Contact your site administrator")
	}
	if err := storage.LinkUserIdentity(userID, c.Issuer, c.Subject, c.Email); err != nil {
		return nil, err
	}
	if storage.IsCompanyMode() && s.GroupsClaim != "" && len(s.TeamMap) > 0 {
		if err := SyncSSOTeams(userID, s.TeamMap, c.Groups); err != nil {
			// The person can still sign in; their teams catch up next time.
			fmt.Printf("sso: team sync for user %d: %v\n", userID, err)
		}
	}
	res.UserID = userID
	return res, nil
}

var usernameUnsafe = regexp.MustCompile(`[^a-z0-9_]+`)

// ssoUsernameBase turns a preferred username or email into a username stem.
func ssoUsernameBase(c SSOClaims) string {
	raw := c.PreferredUsername
	if raw == "" || strings.Contains(raw, "@") {
		raw = c.Email
		if at := strings.Index(raw, "@"); at > 0 {
			raw = raw[:at]
		}
	}
	base := strings.Trim(usernameUnsafe.ReplaceAllString(strings.ToLower(raw), "_"), "_")
	if len(base) > 24 {
		base = base[:24]
	}
	for len(base) < 3 {
		base += "x"
	}
	return base
}

func createSSOAccount(c SSOClaims) (int, error) {
	if err := CheckNewAccountEmail(c.Email); err != nil {
		return 0, ssoDenied("accounts with that email domain can't be created here")
	}
	base := ssoUsernameBase(c)
	userName := ""
	for i := 0; i < 50; i++ {
		candidate := base
		if i > 0 {
			candidate = fmt.Sprintf("%s%d", base, i+1)
		}
		taken, err := storage.UsernameTaken(candidate, 0)
		if err != nil {
			return 0, err
		}
		if !taken {
			userName = candidate
			break
		}
	}
	if userName == "" {
		return 0, ssoDenied("couldn't pick a username for you; ask an admin to create your account")
	}

	// Nobody knows this password: the person signs in through SSO, or sets one
	// with "Forgot password" if the site allows passwords.
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return 0, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(hex.EncodeToString(raw)), bcrypt.DefaultCost)
	if err != nil {
		return 0, err
	}
	roleID, err := storage.GetDefaultRoleID()
	if err != nil {
		return 0, err
	}
	timezone := "America/New_York"
	if settings, err := storage.GetSiteSettings(); err == nil && settings.DefaultTimezone != "" {
		timezone = settings.DefaultTimezone
	}
	// The provider verified the email, so the account starts verified.
	userID, err := storage.RegisterUser(c.Email, string(hash), timezone, userName, roleID, 0)
	if err != nil {
		return 0, err
	}
	first, last := strings.TrimSpace(c.GivenName), strings.TrimSpace(c.FamilyName)
	if first == "" && last == "" && c.Name != "" {
		parts := strings.Fields(c.Name)
		first = parts[0]
		if len(parts) > 1 {
			last = strings.Join(parts[1:], " ")
		}
	}
	if first != "" || last != "" {
		first, _ = normalizePersonName(first, "first name")
		last, _ = normalizePersonName(last, "last name")
		if err := storage.SetUserNames(userID, first, last); err != nil {
			fmt.Printf("sso: names for user %d: %v\n", userID, err)
		}
	}
	return userID, nil
}

// SyncSSOTeams puts userID on the mapped teams for groups and takes them off
// mapped teams for groups they no longer have. Unmapped teams are left alone.
func SyncSSOTeams(userID int, teamMap map[string]int, groups []string) error {
	want := map[int]bool{}
	mapped := map[int]bool{}
	for group, teamID := range teamMap {
		mapped[teamID] = true
		if slices.Contains(groups, group) {
			want[teamID] = true
		}
	}
	var errs []error
	for teamID := range mapped {
		role, err := storage.GetOrganizationRole(teamID, userID)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		switch {
		case want[teamID] && role == "":
			if _, err := storage.AddOrganizationMember(teamID, userID, storage.CompanyMemberRole); err != nil {
				errs = append(errs, fmt.Errorf("add to team %d: %w", teamID, err))
			}
		case !want[teamID] && role != "" && role != "owner":
			if err := storage.RemoveOrganizationMember(teamID, userID); err != nil {
				errs = append(errs, fmt.Errorf("remove from team %d: %w", teamID, err))
			}
		}
	}
	return errors.Join(errs...)
}

// SSOSettingsInput patches the SSO settings. Nil fields stay as they are.
type SSOSettingsInput struct {
	Enabled        *bool
	Label          *string
	IssuerURL      *string
	ClientID       *string
	ClientSecret   *string
	Scopes         *string
	AutoCreate     *bool
	AllowedDomains *[]string
	SSOOnly        *bool
	GroupsClaim    *string
	TeamMap        *map[string]int
}

// UpdateSSOSettings validates and saves a patch. Only admins reach it.
func UpdateSSOSettings(in SSOSettingsInput) (*storage.SSOSettings, error) {
	s, err := storage.GetSSOSettings()
	if err != nil {
		return nil, err
	}
	if in.Label != nil {
		s.Label = strings.TrimSpace(*in.Label)
		if len(s.Label) > 60 {
			return nil, fmt.Errorf("%w: the button label is too long (60 characters at most)", ErrValidation)
		}
	}
	if in.IssuerURL != nil {
		issuer := strings.TrimRight(strings.TrimSpace(*in.IssuerURL), "/")
		if issuer != "" {
			u, err := url.Parse(issuer)
			if err != nil || u.Host == "" || (u.Scheme != "https" && !(u.Scheme == "http" && isLocalHost(u.Hostname()))) {
				return nil, fmt.Errorf("%w: the issuer URL must be an https:// address", ErrValidation)
			}
		}
		s.IssuerURL = issuer
	}
	if in.ClientID != nil {
		s.ClientID = strings.TrimSpace(*in.ClientID)
	}
	if in.ClientSecret != nil {
		if v := strings.TrimSpace(*in.ClientSecret); v == "" {
			s.ClientSecretEnc = ""
		} else {
			enc, err := secret.Encrypt(v)
			if err != nil {
				return nil, err
			}
			s.ClientSecretEnc = enc
		}
	}
	if in.Scopes != nil {
		s.Scopes = strings.Join(strings.Fields(*in.Scopes), " ")
	}
	if in.AutoCreate != nil {
		s.AutoCreate = *in.AutoCreate
	}
	if in.AllowedDomains != nil {
		out := []string{}
		for _, d := range *in.AllowedDomains {
			d = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(d), "@"))
			if d == "" {
				continue
			}
			if strings.ContainsAny(d, " /@") || !strings.Contains(d, ".") {
				return nil, fmt.Errorf("%w: %q isn't an email domain", ErrValidation, d)
			}
			if !slices.Contains(out, d) {
				out = append(out, d)
			}
		}
		s.AllowedDomains = out
	}
	if in.SSOOnly != nil {
		s.SSOOnly = *in.SSOOnly
	}
	if in.GroupsClaim != nil {
		s.GroupsClaim = strings.TrimSpace(*in.GroupsClaim)
	}
	if in.TeamMap != nil {
		m := map[string]int{}
		for group, teamID := range *in.TeamMap {
			group = strings.TrimSpace(group)
			if group == "" {
				continue
			}
			org, err := storage.GetOrganizationByID(teamID)
			if err != nil || org == nil || org.IsCompany {
				return nil, fmt.Errorf("%w: group %q maps to a team that doesn't exist", ErrValidation, group)
			}
			m[group] = teamID
		}
		s.TeamMap = m
	}
	if in.Enabled != nil {
		s.Enabled = *in.Enabled
	}
	if s.Enabled && (s.IssuerURL == "" || s.ClientID == "" || s.ClientSecretEnc == "") {
		return nil, fmt.Errorf("%w: set the issuer URL, client ID, and client secret before turning SSO on", ErrValidation)
	}
	if err := storage.SaveSSOSettings(*s); err != nil {
		return nil, err
	}
	return s, nil
}

func isLocalHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}
