package domain

import (
	"context"
	"errors"
	"testing"

	"GoTodo/internal/storage"
)

const testIssuer = "https://idp.example.com"

func enableTestSSO(t *testing.T, mutate func(*storage.SSOSettings)) *storage.SSOSettings {
	t.Helper()
	s := storage.SSOSettings{
		Enabled: true, IssuerURL: testIssuer, ClientID: "client", ClientSecretEnc: "enc",
		TeamMap: map[string]int{}, AllowedDomains: []string{},
	}
	if mutate != nil {
		mutate(&s)
	}
	if err := storage.SaveSSOSettings(s); err != nil {
		t.Fatal(err)
	}
	got, err := storage.GetSSOSettings()
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestSSOLinksExistingAccountByVerifiedEmail(t *testing.T) {
	owner := setupCompanySite(t, "gotodo_sso_link_test")
	alice := createCompanyAccount(t, owner, "alice", 0)
	s := enableTestSSO(t, nil)

	// An unverified email never links: anyone can claim an address at some providers.
	_, err := ResolveSSOLogin(s, SSOClaims{Issuer: testIssuer, Subject: "sub-a", Email: "alice@example.com"})
	if !errors.Is(err, ErrSSODenied) {
		t.Fatalf("unverified email: want denied, got %v", err)
	}

	res, err := ResolveSSOLogin(s, SSOClaims{Issuer: testIssuer, Subject: "sub-a", Email: "ALICE@example.com", EmailVerified: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.UserID != alice || !res.Linked || res.Created {
		t.Fatalf("got %+v, want link to %d", res, alice)
	}
	// Later sign-ins go by subject, even if the email changed at the provider.
	res, err = ResolveSSOLogin(s, SSOClaims{Issuer: testIssuer, Subject: "sub-a", Email: "renamed@example.com"})
	if err != nil || res.UserID != alice {
		t.Fatalf("by subject: %+v %v", res, err)
	}
}

func TestSSOAutoCreateAndDomains(t *testing.T) {
	setupCompanySite(t, "gotodo_sso_create_test")
	s := enableTestSSO(t, func(s *storage.SSOSettings) { s.AllowedDomains = []string{"acme.com"} })

	claims := SSOClaims{Issuer: testIssuer, Subject: "sub-new", Email: "new.person@acme.com", EmailVerified: true,
		GivenName: "New", FamilyName: "Person"}
	if _, err := ResolveSSOLogin(s, claims); !errors.Is(err, ErrSSODenied) {
		t.Fatalf("auto-create off: want denied, got %v", err)
	}

	s = enableTestSSO(t, func(s *storage.SSOSettings) { s.AllowedDomains = []string{"acme.com"}; s.AutoCreate = true })
	other := claims
	other.Subject, other.Email = "sub-other", "x@other.com"
	if _, err := ResolveSSOLogin(s, other); !errors.Is(err, ErrSSODenied) {
		t.Fatalf("other domain: want denied, got %v", err)
	}

	res, err := ResolveSSOLogin(s, claims)
	if err != nil || !res.Created {
		t.Fatalf("create: %+v %v", res, err)
	}
	p, err := storage.GetUserProfileByID(res.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if p.UserName != "new_person" {
		t.Fatalf("username %q", p.UserName)
	}
	if verified, _ := storage.IsEmailVerified(res.UserID); !verified {
		t.Fatal("SSO accounts should start verified")
	}
	companyID, _ := storage.CompanyOrganizationID()
	if role, _ := storage.GetOrganizationRole(companyID, res.UserID); role == "" {
		t.Fatal("new SSO account should join the company")
	}
}

func TestSSORefusesDeactivatedAccounts(t *testing.T) {
	owner := setupCompanySite(t, "gotodo_sso_banned_test")
	bob := createCompanyAccount(t, owner, "bob", 0)
	s := enableTestSSO(t, nil)
	if err := storage.SetUserBanned(bob, true); err != nil {
		t.Fatal(err)
	}
	_, err := ResolveSSOLogin(s, SSOClaims{Issuer: testIssuer, Subject: "sub-b", Email: "bob@example.com", EmailVerified: true})
	if !errors.Is(err, ErrSSODenied) {
		t.Fatalf("want denied, got %v", err)
	}
}

func TestSSOSyncsMappedTeams(t *testing.T) {
	owner := setupCompanySite(t, "gotodo_sso_teams_test")
	carol := createCompanyAccount(t, owner, "carol", 0)
	eng, err := CreateOrganizationForUser(context.Background(), owner, "Engineering", "")
	if err != nil {
		t.Fatal(err)
	}
	ops, err := CreateOrganizationForUser(context.Background(), owner, "Ops", "")
	if err != nil {
		t.Fatal(err)
	}
	manual, err := CreateOrganizationForUser(context.Background(), owner, "Manual", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.AddOrganizationMember(manual.ID, carol, storage.CompanyMemberRole); err != nil {
		t.Fatal(err)
	}
	s := enableTestSSO(t, func(s *storage.SSOSettings) {
		s.GroupsClaim = "groups"
		s.TeamMap = map[string]int{"eng": eng.ID, "ops": ops.ID}
	})
	claims := SSOClaims{Issuer: testIssuer, Subject: "sub-c", Email: "carol@example.com", EmailVerified: true, Groups: []string{"eng"}}
	if _, err := ResolveSSOLogin(s, claims); err != nil {
		t.Fatal(err)
	}
	onTeam := func(id int) bool {
		role, _ := storage.GetOrganizationRole(id, carol)
		return role != ""
	}
	if !onTeam(eng.ID) || onTeam(ops.ID) || !onTeam(manual.ID) {
		t.Fatalf("after eng: eng=%v ops=%v manual=%v", onTeam(eng.ID), onTeam(ops.ID), onTeam(manual.ID))
	}
	claims.Groups = []string{"ops"}
	if _, err := ResolveSSOLogin(s, claims); err != nil {
		t.Fatal(err)
	}
	if onTeam(eng.ID) || !onTeam(ops.ID) || !onTeam(manual.ID) {
		t.Fatalf("after ops: eng=%v ops=%v manual=%v", onTeam(eng.ID), onTeam(ops.ID), onTeam(manual.ID))
	}
}

func TestSSOOnlyKeepsAdminPasswords(t *testing.T) {
	owner := setupCompanySite(t, "gotodo_sso_only_test")
	dave := createCompanyAccount(t, owner, "dave", 0)
	enableTestSSO(t, func(s *storage.SSOSettings) { s.SSOOnly = true })
	if storage.SSOOnlyFor(owner) {
		t.Fatal("admins keep password sign-in")
	}
	if !storage.SSOOnlyFor(dave) {
		t.Fatal("members should be SSO only")
	}
}

func TestUpdateSSOSettingsValidates(t *testing.T) {
	setupCompanySite(t, "gotodo_sso_settings_test")
	on := true
	if _, err := UpdateSSOSettings(SSOSettingsInput{Enabled: &on}); !errors.Is(err, ErrValidation) {
		t.Fatalf("enabling without config: %v", err)
	}
	bad := "http://idp.example.com"
	if _, err := UpdateSSOSettings(SSOSettingsInput{IssuerURL: &bad}); !errors.Is(err, ErrValidation) {
		t.Fatalf("http issuer: %v", err)
	}
	domains := []string{"@Acme.com", "acme.com", ""}
	s, err := UpdateSSOSettings(SSOSettingsInput{AllowedDomains: &domains})
	if err != nil || len(s.AllowedDomains) != 1 || s.AllowedDomains[0] != "acme.com" {
		t.Fatalf("domains: %+v %v", s, err)
	}
}

func TestSSOUsernameBase(t *testing.T) {
	cases := map[string]SSOClaims{
		"jdoe":     {PreferredUsername: "jdoe"},
		"jane_doe": {Email: "Jane.Doe@acme.com"},
		"abx":      {PreferredUsername: "ab"},
		"mary_ann": {PreferredUsername: "mary@acme.com", Email: "mary-ann@acme.com"},
	}
	for want, c := range cases {
		if got := ssoUsernameBase(c); got != want {
			t.Errorf("%+v: got %q want %q", c, got, want)
		}
	}
}
