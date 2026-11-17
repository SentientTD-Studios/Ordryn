package domain

import (
	"context"
	"errors"
	"testing"

	"GoTodo/internal/storage"
)

func resetPolicy(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		_ = storage.SaveSecurityPolicy(storage.DefaultSecurityPolicy())
	})
}

func TestSecurityPolicyPasswordLengthAndDomains(t *testing.T) {
	setupCompanySite(t, "gotodo_security_policy_test")
	resetPolicy(t)

	if err := ValidatePassword("12345678"); err != nil {
		t.Fatalf("default minimum: %v", err)
	}
	n := 12
	domains := []string{"Acme.com", "@acme.com"}
	p, err := UpdateSecurityPolicy(SecurityPolicyInput{MinPasswordLength: &n, AllowedEmailDomains: &domains})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.AllowedEmailDomains) != 1 || p.AllowedEmailDomains[0] != "acme.com" {
		t.Fatalf("domains: %v", p.AllowedEmailDomains)
	}
	if err := ValidatePassword("12345678"); !errors.Is(err, ErrValidation) {
		t.Fatalf("12 chars needed: %v", err)
	}
	if err := ValidatePassword("123456789012"); err != nil {
		t.Fatal(err)
	}
	if err := CheckNewAccountEmail("a@other.com"); !errors.Is(err, ErrValidation) {
		t.Fatalf("other domain: %v", err)
	}
	if err := CheckNewAccountEmail("a@ACME.com"); err != nil {
		t.Fatal(err)
	}

	tooShort := 4
	if _, err := UpdateSecurityPolicy(SecurityPolicyInput{MinPasswordLength: &tooShort}); !errors.Is(err, ErrValidation) {
		t.Fatalf("below floor: %v", err)
	}
	idle := 2
	if _, err := UpdateSecurityPolicy(SecurityPolicyInput{SessionIdleMinutes: &idle}); !errors.Is(err, ErrValidation) {
		t.Fatalf("idle too short: %v", err)
	}
}

func TestPeopleManagersRespectAllowedDomains(t *testing.T) {
	owner := setupCompanySite(t, "gotodo_security_policy_people_test")
	resetPolicy(t)
	domains := []string{"acme.com"}
	if _, err := UpdateSecurityPolicy(SecurityPolicyInput{AllowedEmailDomains: &domains}); err != nil {
		t.Fatal(err)
	}
	_, err := CreateAccountForUser(context.Background(), owner, CreateAccountInput{
		FirstName: "Out", LastName: "Side", Email: "out@elsewhere.com", UserName: "outside",
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("want validation error, got %v", err)
	}
}

func TestMFARequirementByRole(t *testing.T) {
	owner := setupCompanySite(t, "gotodo_security_policy_mfa_test")
	resetPolicy(t)
	member := createCompanyAccount(t, owner, "erin", 0)
	profile, err := storage.GetUserProfileByID(member)
	if err != nil {
		t.Fatal(err)
	}

	if storage.MFAEnrollmentRequired(member) {
		t.Fatal("off by default")
	}
	mode := storage.MFARequireRoles
	if _, err := UpdateSecurityPolicy(SecurityPolicyInput{RequireMFA: &mode}); !errors.Is(err, ErrValidation) {
		t.Fatalf("roles mode needs roles: %v", err)
	}
	roles := []int{profile.RoleID}
	if _, err := UpdateSecurityPolicy(SecurityPolicyInput{RequireMFA: &mode, MFARoleIDs: &roles}); err != nil {
		t.Fatal(err)
	}
	if !storage.MFAEnrollmentRequired(member) {
		t.Fatal("member's role requires MFA")
	}
	ownerProfile, _ := storage.GetUserProfileByID(owner)
	if ownerProfile.RoleID != profile.RoleID && storage.MFAEnrollmentRequired(owner) {
		t.Fatal("other roles are not affected")
	}
	if err := DisableMFA(member, "000000"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("can't turn MFA off while required: %v", err)
	}
}
