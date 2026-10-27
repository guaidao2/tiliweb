package site

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"tiliweb/internal/config"
	"tiliweb/internal/markdown"
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

type Contact struct {
	Phone           string `json:"phone"`
	Email           string `json:"email"`
	Address         string `json:"address"`
	MapURL          string `json:"map_url"`
	WeChatQRMediaID int64  `json:"wechat_qr_media_id"`
}

type SEO struct {
	DefaultTitle       string `json:"default_title"`
	DefaultDescription string `json:"default_description"`
	OGImageMediaID     int64  `json:"og_image_media_id"`
	RobotsExtra        string `json:"robots_extra"`
}

type Home struct {
	ShowBanners   bool   `json:"show_banners"`
	LatestCount   int    `json:"latest_count"`
	FeaturedCount int    `json:"featured_count"`
	CTATitle      string `json:"cta_title"`
	CTAText       string `json:"cta_text"`
	CTAURL        string `json:"cta_url"`
}

type Settings struct {
	SiteName        string
	SiteDescription string
	Contact         Contact
	SEO             SEO
	Home            Home
	ContactPageMD   string
	ContactPageHTML string
}

func Defaults(cfg *config.Config) Settings {
	return Settings{
		SiteName:        cfg.Site.Name,
		SiteDescription: cfg.Site.Description,
		Contact: Contact{
			Phone: cfg.Site.Contact.Phone, Email: cfg.Site.Contact.Email,
			Address: cfg.Site.Contact.Address, MapURL: cfg.Site.Contact.MapURL,
			WeChatQRMediaID: cfg.Site.Contact.WeChatQRMediaID,
		},
		SEO: SEO{
			DefaultTitle: cfg.Site.SEO.DefaultTitle, DefaultDescription: cfg.Site.SEO.DefaultDescription,
			OGImageMediaID: cfg.Site.SEO.OGImageMediaID, RobotsExtra: cfg.Site.SEO.RobotsExtra,
		},
		Home: Home{
			ShowBanners: true, LatestCount: 6, FeaturedCount: 3,
			CTATitle: "Talk to us", CTAText: "Tell us about your project.", CTAURL: "/contact",
		},
	}
}

func (s *Store) Load(ctx context.Context, cfg *config.Config) (Settings, error) {
	out := Defaults(cfg)
	if err := s.getJSON(ctx, "site.name", &out.SiteName); err != nil {
		return out, err
	}
	if err := s.getJSON(ctx, "site.description", &out.SiteDescription); err != nil {
		return out, err
	}
	if err := s.getJSON(ctx, "site.contact", &out.Contact); err != nil {
		return out, err
	}
	if err := s.getJSON(ctx, "site.seo", &out.SEO); err != nil {
		return out, err
	}
	if err := s.getJSON(ctx, "home", &out.Home); err != nil {
		return out, err
	}
	if err := s.getJSON(ctx, "contact_page", &out.ContactPageMD); err != nil {
		return out, err
	}
	if out.SiteName == "" {
		out.SiteName = cfg.Site.Name
	}
	if out.Home.LatestCount <= 0 {
		out.Home.LatestCount = 6
	}
	if out.Home.FeaturedCount <= 0 {
		out.Home.FeaturedCount = 3
	}
	out.ContactPageHTML, _ = markdown.Render(out.ContactPageMD)
	return out, nil
}

func (s *Store) Save(ctx context.Context, v Settings) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	set := func(key string, value any) error {
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO settings(key, value) VALUES(?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, string(raw))
		return err
	}
	if err := set("site.name", v.SiteName); err != nil {
		return err
	}
	if err := set("site.description", v.SiteDescription); err != nil {
		return err
	}
	if err := set("site.contact", v.Contact); err != nil {
		return err
	}
	if err := set("site.seo", v.SEO); err != nil {
		return err
	}
	if err := set("home", v.Home); err != nil {
		return err
	}
	if err := set("contact_page", v.ContactPageMD); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Get(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

func (s *Store) SetJSON(ctx context.Context, key string, value any) error {
	return s.setJSON(ctx, key, value)
}

func (s *Store) getJSON(ctx context.Context, key string, dst any) error {
	raw, err := s.Get(ctx, key)
	if err != nil || raw == "" {
		return err
	}
	if err := json.Unmarshal([]byte(raw), dst); err != nil {
		return fmt.Errorf("settings %s: %w", key, err)
	}
	return nil
}

func (s *Store) setJSON(ctx context.Context, key string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO settings(key, value) VALUES(?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, string(raw))
	return err
}
