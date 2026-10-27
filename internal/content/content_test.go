package content

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"tiliweb/internal/store/sqlite"
)

func TestContentLifecycle(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "content.db"), 4, 5000)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := sqlite.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	res, err := db.ExecContext(ctx, `INSERT INTO admin_users(username, password_hash, display_name, role, status, created_at, updated_at)
		VALUES('admin', 'x', 'Admin', 'superadmin', 'active', 1, 1)`)
	if err != nil {
		t.Fatal(err)
	}
	adminID, _ := res.LastInsertId()
	svc := NewService(db)

	ch, err := svc.CreateChannel(ctx, &Channel{
		Slug: "news", Name: "News", Type: "article", Status: "active", ShowInNav: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateChannel(ctx, &Channel{Slug: "news", Name: "Dup", Type: "article"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate channel = %v", err)
	}

	e, err := svc.CreateEntry(ctx, adminID, EntryInput{
		ChannelID: ch.ID,
		Slug:      "hello-world",
		Title:     "Hello World",
		Summary:   "sum",
		BodyMD:    "# Hi\n\n**bold**",
		Status:    "published",
		Tags:      []string{"Go", "Web"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if e.Slug != "hello-world" || e.Status != "published" || len(e.Tags) != 2 {
		t.Fatalf("entry = %+v", e)
	}
	if !contains(e.BodyHTML, "<h1") || !contains(e.BodyHTML, "<strong>") {
		t.Fatalf("html = %s", e.BodyHTML)
	}

	pub, err := svc.PublicEntry(ctx, "news", "hello-world")
	if err != nil {
		t.Fatal(err)
	}
	if pub.ID != e.ID {
		t.Fatalf("public entry = %d", pub.ID)
	}

	updated, err := svc.UpdateEntry(ctx, adminID, e.ID, EntryInput{
		ChannelID: ch.ID,
		Slug:      "hello-world",
		Title:     "Hello Again",
		BodyMD:    "## Updated",
		Status:    "published",
		Tags:      []string{"Go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != "Hello Again" || !contains(updated.BodyHTML, "<h2") {
		t.Fatalf("updated = %+v", updated)
	}
	revs, err := svc.Revisions(ctx, "entry", e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) != 1 || revs[0].Title != "Hello World" {
		t.Fatalf("revisions = %+v", revs)
	}

	if err := svc.SetEntryStatus(ctx, adminID, e.ID, "draft"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PublicEntry(ctx, "news", "hello-world"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("draft public = %v", err)
	}
	if err := svc.SetEntryStatus(ctx, adminID, e.ID, "published"); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteEntry(ctx, e.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PublicEntry(ctx, "news", "hello-world"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted public = %v", err)
	}
	if err := svc.RestoreEntry(ctx, e.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PublicEntry(ctx, "news", "hello-world"); err != nil {
		t.Fatal(err)
	}

	page, err := svc.CreatePage(ctx, adminID, PageInput{
		Slug: "about", Title: "About", BodyMD: "About **us**", ShowInNav: true, Status: "published",
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.BodyMD != "About **us**" || !contains(page.BodyHTML, "<strong>") {
		t.Fatalf("page = %+v", page)
	}
	if _, err := svc.UpdatePage(ctx, adminID, page.ID, PageInput{
		Slug: "about", Title: "About Us", BodyMD: "Updated", ShowInNav: true, Status: "published",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PageBySlug(ctx, "about"); err != nil {
		t.Fatal(err)
	}
	prevs, err := svc.Revisions(ctx, "page", page.ID)
	if err != nil || len(prevs) != 1 {
		t.Fatalf("page revisions = %+v, %v", prevs, err)
	}

	channels, entries, pages, media, err := svc.Counts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if channels != 1 || entries != 1 || pages != 1 || media != 0 {
		t.Fatalf("counts = %d/%d/%d/%d", channels, entries, pages, media)
	}

	if _, err := svc.UpdateEntry(ctx, adminID, e.ID, EntryInput{ChannelID: ch.ID, Title: "", Status: "published"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid entry = %v", err)
	}
	if got := Slugify("Hello World! 中文"); got != "hello-world" {
		t.Fatalf("slug = %q", got)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
