package language

import "testing"

func TestDetectEnglishConfig(t *testing.T) {
	result := Detect("This document explains how we deploy staging and configure OAuth callbacks for the API server.")
	if result.Code != "en" {
		t.Fatalf("expected en, got %q", result.Code)
	}
	if result.Config != "english" {
		t.Fatalf("expected english config, got %q", result.Config)
	}
}

func TestDetectFrenchConfig(t *testing.T) {
	result := Detect("Ce document explique comment nous déployons l'application et corrigeons la redirection OAuth en production.")
	if result.Code != "fr" {
		t.Fatalf("expected fr, got %q", result.Code)
	}
	if result.Config != "french" {
		t.Fatalf("expected french config, got %q", result.Config)
	}
}

func TestDetectCodeHeavyFallsBackToSimple(t *testing.T) {
	result := Detect("func main() { if err != nil { return err } // TODO: fix redirect_url }")
	if result.Code != UnknownCode {
		t.Fatalf("expected und, got %q", result.Code)
	}
	if result.Config != SimpleConfig {
		t.Fatalf("expected simple config, got %q", result.Config)
	}
}
