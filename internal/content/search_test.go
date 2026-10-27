package content

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"tiliweb/internal/store/sqlite"
)

func TestSearchAndBanners(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "search.db"), 4, 5000)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := sqlite.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	res, err := db.ExecContext(ctx, `INSERT INTO admin_users(username,password_hash,display_name,role,status,created_at,updated_at)
		VALUES('admin','x','Admin','superadmin','active',1,1)`)
	if err != nil {
		t.Fatal(err)
	}
	adminID, _ := res.LastInsertId()
	svc := NewService(db)
	ch, err := svc.CreateChannel(ctx, &Channel{Slug: "news", Name: "News", Type: "article", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := svc.CreateEntry(ctx, adminID, EntryInput{
		ChannelID: ch.ID, Slug: "manual", Title: "产品手册", Summary: "产品说明",
		BodyMD: "这是产品使用说明。", Status: "published",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := NormalizeSearch("产品手册"); got != "产 品 手 册" {
		t.Fatalf("normalize = %q", got)
	}
	results, err := svc.Search(ctx, "产品", "article", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Type != "entry" || results[0].URL != "/news/manual" {
		t.Fatalf("results = %+v", results)
	}
	if _, err := svc.CreatePage(ctx, adminID, PageInput{
		Slug: "about-us", Title: "关于我们", BodyMD: "关于公司的介绍", Status: "published",
	}); err != nil {
		t.Fatal(err)
	}
	pages, err := svc.Search(ctx, "关于", "page", 10, 0)
	if err != nil || len(pages) != 1 || pages[0].URL != "/about-us" {
		t.Fatalf("pages = %+v, %v", pages, err)
	}

	if err := svc.DeleteEntry(ctx, entry.ID); err != nil {
		t.Fatal(err)
	}
	results, err = svc.Search(ctx, "产品", "article", 10, 0)
	if err != nil || len(results) != 0 {
		t.Fatalf("deleted search = %+v, %v", results, err)
	}
	if err := svc.RestoreEntry(ctx, entry.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.RebuildSearch(ctx); err != nil {
		t.Fatal(err)
	}
	results, err = svc.Search(ctx, "产品", "article", 10, 0)
	if err != nil || len(results) != 1 {
		t.Fatalf("rebuilt search = %+v, %v", results, err)
	}

	b, err := svc.CreateBanner(ctx, BannerInput{Title: "Welcome", LinkURL: "/contact", ButtonText: "Contact", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if list, err := svc.ListBanners(ctx, true); err != nil || len(list) != 1 {
		t.Fatalf("banners = %+v, %v", list, err)
	}
	if _, err := svc.UpdateBanner(ctx, b.ID, BannerInput{Title: "Welcome", LinkURL: "javascript:alert(1)", Enabled: true}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid link = %v", err)
	}
	if _, err := svc.UpdateBanner(ctx, b.ID, BannerInput{Title: "Welcome", LinkURL: "/contact", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if list, err := svc.ListBanners(ctx, true); err != nil || len(list) != 0 {
		t.Fatalf("disabled banners = %+v, %v", list, err)
	}
	if err := svc.DeleteBanner(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
}
