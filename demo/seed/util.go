package main

import (
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
)

// isAlreadyExists lets every seed*  function be safely re-run against a
// cluster that was already seeded, instead of failing on the second run.
func isAlreadyExists(err error) bool { return errors.IsAlreadyExists(err) }

func int32p(v int32) *int32 { return &v }

// mustParseQuantity is only ever called with our own hardcoded size strings
// (see builders.go), so a parse failure means a typo in this program, not
// bad external input — panicking here is the right way to catch that in
// tests/CI rather than silently seeding a broken PVC.
func mustParseQuantity(s string) resource.Quantity { return resource.MustParse(s) }
