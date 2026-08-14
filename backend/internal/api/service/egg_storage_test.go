package service

import (
	"strings"
	"testing"
)

func TestBuildEggArtifactObjectKey_UsesEggsRoot(t *testing.T) {
	got := buildEggArtifactObjectKey("translator-pro", "persona.zip")
	if got != "eggs/translator-pro/persona.zip" {
		t.Fatalf("expected eggs root object key, got %q", got)
	}
}

func TestBuildAdminEggVersionUploadObjectKey_UsesEggsRoot(t *testing.T) {
	got := buildAdminEggVersionUploadObjectKey("translator-pro", 3, "openclaw.zip")
	if got != "eggs/translator-pro/3_persona.zip" {
		t.Fatalf("expected versioned persona object key, got %q", got)
	}
	if strings.Contains(got, "/media/eggs/") {
		t.Fatalf("expected object key without media prefix, got %q", got)
	}
}

func TestBuildAdminEggVersionUploadObjectKey_MapsSkillRole(t *testing.T) {
	got := buildAdminEggVersionUploadObjectKey("translator-pro", 5, "skill.zip")
	if got != "eggs/translator-pro/5_skill.zip" {
		t.Fatalf("expected versioned skill object key, got %q", got)
	}
}
