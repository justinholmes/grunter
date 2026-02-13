package rfc

import "time"

// CheckResult holds the outcome of an RFC/change request verification.
type CheckResult struct {
	Approved      bool
	Emergency     bool
	RFCIdentifier string
	Message       string
}

// WindowResult holds the outcome of a deployment window check.
type WindowResult struct {
	Allowed         bool
	Message         string
	NextWindowStart *time.Time
}

// Checker verifies that a deployment has an approved RFC/change request.
type Checker interface {
	CheckRFC() (*CheckResult, error)
}
