package model

import "testing"

func TestWidgetSiteTableName(t *testing.T) {
	site := WidgetSite{}
	if site.TableName() != "widget_sites" {
		t.Fatalf("expected table name 'widget_sites', got %q", site.TableName())
	}
}

func TestWidgetSessionTableName(t *testing.T) {
	session := WidgetSession{}
	if session.TableName() != "widget_sessions" {
		t.Fatalf("expected table name 'widget_sessions', got %q", session.TableName())
	}
}

func TestWidgetStatusConstants(t *testing.T) {
	if WidgetSiteStatusActive == WidgetSiteStatusDisabled {
		t.Fatal("widget site status constants should not collide")
	}
	if WidgetSessionStatusActive == WidgetSessionStatusClosed || WidgetSessionStatusClosed == WidgetSessionStatusBanned {
		t.Fatal("widget session status constants should not collide")
	}
}
