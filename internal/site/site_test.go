package site

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"tiliweb/internal/config"
	"tiliweb/internal/store/sqlite"
)

func TestSettingsAndContactMessages(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "site.db"), 4, 5000)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := sqlite.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.Site.Name = "Config Site"
	cfg.Site.Contact.Email = "hello@example.com"
	st := NewStore(db)

	got, err := st.Load(ctx, &cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got.SiteName != "Config Site" || got.Contact.Email != "hello@example.com" || got.Home.LatestCount != 6 {
		t.Fatalf("defaults = %+v", got)
	}

	got.SiteName = "Tiliweb"
	got.SiteDescription = "A Go CMS"
	got.Contact.Phone = "123"
	got.Home.LatestCount = 4
	got.Home.CTAURL = "/contact"
	got.ContactPageMD = "# Contact\n\nEmail us."
	if err := st.Save(ctx, got); err != nil {
		t.Fatal(err)
	}
	loaded, err := st.Load(ctx, &cfg)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SiteName != "Tiliweb" || loaded.Home.LatestCount != 4 || loaded.Contact.Phone != "123" {
		t.Fatalf("loaded = %+v", loaded)
	}
	if !strings.Contains(loaded.ContactPageHTML, "<h1") {
		t.Fatalf("contact html = %s", loaded.ContactPageHTML)
	}

	m, err := st.CreateContactMessage(ctx, &ContactMessage{
		Name: "Alice", Email: "alice@example.com", Message: "Hello", SourcePage: "/contact", IP: "127.0.0.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.Status != "new" {
		t.Fatalf("status = %q", m.Status)
	}
	list, err := st.ListContactMessages(ctx, "new", 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	n, err := st.CountNewContactMessages(ctx)
	if err != nil || n != 1 {
		t.Fatalf("count = %d, %v", n, err)
	}
	if err := st.UpdateContactMessage(ctx, m.ID, "replied", "called back"); err != nil {
		t.Fatal(err)
	}
	m2, err := st.ContactMessageByID(ctx, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if m2.Status != "replied" || m2.AdminNote != "called back" {
		t.Fatalf("message = %+v", m2)
	}
	if err := st.UpdateContactMessage(ctx, m.ID, "bogus", ""); err != ErrInvalidStatus {
		t.Fatalf("invalid status = %v", err)
	}
}
