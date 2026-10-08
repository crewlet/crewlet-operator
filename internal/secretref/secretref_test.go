package secretref

import (
	"slices"
	"testing"
)

// TestCompareOrdersEveryField pins the order every rendered list of secret
// references relies on: namespace first, then the Secret, then the key.
func TestCompareOrdersEveryField(t *testing.T) {
	keys := []Key{
		{Namespace: "b", Name: "a", Key: "a"},
		{Namespace: "a", Name: "b", Key: "a"},
		{Namespace: "a", Name: "a", Key: "b"},
		{Namespace: "a", Name: "a", Key: "a"},
	}
	slices.SortFunc(keys, Compare)
	want := []Key{
		{Namespace: "a", Name: "a", Key: "a"},
		{Namespace: "a", Name: "a", Key: "b"},
		{Namespace: "a", Name: "b", Key: "a"},
		{Namespace: "b", Name: "a", Key: "a"},
	}
	if !slices.Equal(keys, want) {
		t.Fatalf("sorted = %v, want %v", keys, want)
	}
	if Compare(want[0], want[0]) != 0 {
		t.Fatal("a key must compare equal to itself")
	}
}

// TestStringNamesTheKeyNotTheValue documents the log form.
func TestStringNamesTheKeyNotTheValue(t *testing.T) {
	if got := (Key{Namespace: "ns", Name: "slack", Key: "botToken"}).String(); got != "ns/slack[botToken]" {
		t.Fatalf("String() = %q", got)
	}
}
