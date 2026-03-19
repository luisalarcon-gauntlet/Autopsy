// Package auth provides hardcoded demo user accounts and HTTP context helpers.
package auth

import (
	"context"
	"strings"
)

const (
	RoleISV      = "isv"
	RolePlatform = "platform"
)

// User is a demo login account.
type User struct {
	Username string
	Name     string
	Org      string
	OrgID    string // lowercase org identifier used as DB org_id (e.g. "astronomer")
	Role     string
	Initials string
}

// Customer is a customer entry for ISV users.
type Customer struct {
	Name          string
	SeverityScore int
	Health        string // critical | warning | healthy
}

// Slug returns the URL-safe slug for this customer name,
// e.g. "Goldman Sachs" → "goldman-sachs".
func (c Customer) Slug() string {
	return strings.ToLower(strings.ReplaceAll(c.Name, " ", "-"))
}

// InboxRow is a bundle-inbox entry for platform users.
type InboxRow struct {
	ISV      string
	Customer string
	Severity int
	Health   string
	TopIssue string
	Age      string
}

// Users is the complete set of demo accounts keyed by username.
var Users = map[string]User{
	"airbyte": {
		Username: "airbyte",
		Name:     "Sarah Chen",
		Org:      "Airbyte",
		OrgID:    "airbyte",
		Role:     RoleISV,
		Initials: "SC",
	},
	"replicated": {
		Username: "replicated",
		Name:     "Marcus Johnson",
		Org:      "Replicated",
		OrgID:    "replicated",
		Role:     RolePlatform,
		Initials: "MJ",
	},
	"alex": {
		Username: "alex",
		Name:     "Alex Rivera",
		Org:      "Astronomer",
		OrgID:    "astronomer",
		Role:     RoleISV,
		Initials: "AR",
	},
}

// ISVCustomers maps an ISV username to their customer list.
var ISVCustomers = map[string][]Customer{
	"airbyte": {
		{Name: "Toyota", SeverityScore: 85, Health: "critical"},
		{Name: "Nike", SeverityScore: 12, Health: "healthy"},
		{Name: "Goldman Sachs", SeverityScore: 44, Health: "warning"},
	},
	"alex": {
		{Name: "Chevron", SeverityScore: 61, Health: "warning"},
		{Name: "Barclays", SeverityScore: 28, Health: "healthy"},
		{Name: "Deutsche Bank", SeverityScore: 72, Health: "critical"},
	},
}

// PlatformPartners is the list of ISV partner names shown to platform users.
var PlatformPartners = []string{"Airbyte", "Astronomer", "DataStax"}

// PlatformInbox is the hardcoded bundle inbox for platform users.
var PlatformInbox = []InboxRow{
	{ISV: "Airbyte", Customer: "Toyota", Severity: 85, Health: "critical", TopIssue: "CrashLoopBackOff in payment-processor", Age: "5m ago"},
	{ISV: "Airbyte", Customer: "Goldman Sachs", Severity: 44, Health: "warning", TopIssue: "Memory pressure on 2 nodes", Age: "1h ago"},
	{ISV: "Astronomer", Customer: "Chevron", Severity: 61, Health: "warning", TopIssue: "ImagePullBackOff: registry timeout", Age: "3h ago"},
	{ISV: "DataStax", Customer: "Barclays", Severity: 8, Health: "healthy", TopIssue: "—", Age: "6h ago"},
	{ISV: "Airbyte", Customer: "Nike", Severity: 12, Health: "healthy", TopIssue: "—", Age: "1d ago"},
}

type ctxKey struct{}

// WithUser returns a new context carrying u.
func WithUser(ctx context.Context, u User) context.Context {
	return context.WithValue(ctx, ctxKey{}, u)
}

// FromContext extracts the User from ctx. Returns zero value and false if absent.
func FromContext(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(ctxKey{}).(User)
	return u, ok
}
