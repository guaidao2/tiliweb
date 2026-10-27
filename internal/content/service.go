package content

import (
	"context"
	"database/sql"
	"net/url"
	"regexp"
	"strings"
	"unicode"

	"tiliweb/internal/markdown"
)

type Service struct {
	store *Store
}

func NewService(db *sql.DB) *Service {
	return &Service{store: NewStore(db)}
}

func (s *Service) Store() *Store { return s.store }

func (s *Service) CreateChannel(ctx context.Context, c *Channel) (*Channel, error) {
	if err := normalizeChannel(c); err != nil {
		return nil, err
	}
	return s.store.CreateChannel(ctx, c)
}

func (s *Service) UpdateChannel(ctx context.Context, c *Channel) (*Channel, error) {
	if err := normalizeChannel(c); err != nil {
		return nil, err
	}
	return s.store.UpdateChannel(ctx, c)
}

func normalizeChannel(c *Channel) error {
	c.Slug = Slugify(c.Slug)
	if c.Slug == "" {
		return ErrInvalid
	}
	if strings.TrimSpace(c.Name) == "" {
		return ErrInvalid
	}
	switch c.Type {
	case "article", "product", "case", "job", "download":
	default:
		return ErrInvalid
	}
	if c.Status == "" {
		c.Status = "active"
	}
	switch c.Status {
	case "active", "hidden":
	default:
		return ErrInvalid
	}
	if c.ParentID != 0 && c.ParentID == c.ID {
		return ErrInvalid
	}
	return nil
}

func (s *Service) CreateEntry(ctx context.Context, adminID int64, in EntryInput) (*Entry, error) {
	if in.ChannelID == 0 || strings.TrimSpace(in.Title) == "" {
		return nil, ErrInvalid
	}
	if _, err := s.store.ChannelByID(ctx, in.ChannelID); err != nil {
		return nil, err
	}
	status, err := normalizeEntryStatus(in.Status)
	if err != nil {
		return nil, err
	}
	html, _ := markdown.Render(in.BodyMD)
	e := &Entry{
		ChannelID:     in.ChannelID,
		Slug:          Slugify(in.Slug),
		Title:         strings.TrimSpace(in.Title),
		Summary:       strings.TrimSpace(in.Summary),
		BodyMD:        in.BodyMD,
		BodyHTML:      html,
		CoverMediaID:  in.CoverMediaID,
		AuthorAdminID: adminID,
		Status:        status,
		Pinned:        in.Pinned,
		Featured:      in.Featured,
		SortOrder:     in.SortOrder,
		ExtraJSON:     in.ExtraJSON,
	}
	created, err := s.store.CreateEntry(ctx, e)
	if err != nil {
		return nil, err
	}
	if err := s.store.SetEntryTags(ctx, created.ID, tagModels(in.Tags)); err != nil {
		return nil, err
	}
	return s.GetEntry(ctx, created.ID)
}

func (s *Service) UpdateEntry(ctx context.Context, adminID, id int64, in EntryInput) (*Entry, error) {
	old, err := s.store.EntryByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if in.ChannelID == 0 || strings.TrimSpace(in.Title) == "" {
		return nil, ErrInvalid
	}
	status, err := normalizeEntryStatus(in.Status)
	if err != nil {
		return nil, err
	}
	html, _ := markdown.Render(in.BodyMD)
	old.ChannelID = in.ChannelID
	old.Slug = Slugify(in.Slug)
	old.Title = strings.TrimSpace(in.Title)
	old.Summary = strings.TrimSpace(in.Summary)
	old.BodyMD = in.BodyMD
	old.BodyHTML = html
	old.CoverMediaID = in.CoverMediaID
	old.Status = status
	old.Pinned = in.Pinned
	old.Featured = in.Featured
	old.SortOrder = in.SortOrder
	old.ExtraJSON = in.ExtraJSON
	if _, err := s.store.UpdateEntry(ctx, old, adminID); err != nil {
		return nil, err
	}
	if err := s.store.SetEntryTags(ctx, id, tagModels(in.Tags)); err != nil {
		return nil, err
	}
	return s.GetEntry(ctx, id)
}

func (s *Service) ListChannels(ctx context.Context, includeHidden bool) ([]Channel, error) {
	return s.store.ListChannels(ctx, includeHidden)
}

func (s *Service) ChannelByID(ctx context.Context, id int64) (*Channel, error) {
	return s.store.ChannelByID(ctx, id)
}

func (s *Service) ChannelBySlug(ctx context.Context, slug string) (*Channel, error) {
	return s.store.ChannelBySlug(ctx, slug)
}

func (s *Service) DeleteChannel(ctx context.Context, id int64) error {
	return s.store.DeleteChannel(ctx, id)
}

func (s *Service) GetEntry(ctx context.Context, id int64) (*Entry, error) {
	e, err := s.store.EntryByID(ctx, id)
	if err != nil {
		return nil, err
	}
	e.Tags, _ = s.store.EntryTags(ctx, e.ID)
	return e, nil
}

func (s *Service) PublicEntry(ctx context.Context, channelSlug, key string) (*Entry, error) {
	e, err := s.store.EntryByChannelSlug(ctx, channelSlug, key, true)
	if err != nil {
		return nil, err
	}
	e.Tags, _ = s.store.EntryTags(ctx, e.ID)
	return e, nil
}

func (s *Service) PublicEntries(ctx context.Context, channelSlug string, limit, offset int) ([]Entry, error) {
	return s.store.PublicEntries(ctx, channelSlug, limit, offset)
}

func (s *Service) PublicLatestEntries(ctx context.Context, limit int) ([]Entry, error) {
	return s.store.PublicLatestEntries(ctx, limit)
}

func (s *Service) PublicFeaturedEntries(ctx context.Context, limit int) ([]Entry, error) {
	return s.store.PublicFeaturedEntries(ctx, limit)
}

func (s *Service) PublicEntriesByType(ctx context.Context, typ string, limit int) ([]Entry, error) {
	return s.store.PublicEntriesByType(ctx, typ, limit)
}

func (s *Service) PublicPages(ctx context.Context) ([]Page, error) {
	return s.store.PublicPages(ctx)
}

func (s *Service) ListEntries(ctx context.Context, channelID int64, status string, includeDeleted bool, limit, offset int) ([]Entry, error) {
	return s.store.ListEntries(ctx, channelID, status, includeDeleted, limit, offset)
}

func (s *Service) SetEntryStatus(ctx context.Context, adminID, id int64, status string) error {
	status, err := normalizeEntryStatus(status)
	if err != nil {
		return err
	}
	return s.store.SetEntryStatus(ctx, id, status, adminID)
}

func (s *Service) DeleteEntry(ctx context.Context, id int64) error {
	return s.store.SoftDeleteEntry(ctx, id)
}

func (s *Service) RestoreEntry(ctx context.Context, id int64) error {
	return s.store.RestoreEntry(ctx, id)
}

func (s *Service) EntryTags(ctx context.Context, id int64) ([]Tag, error) {
	return s.store.EntryTags(ctx, id)
}

func (s *Service) CreatePage(ctx context.Context, adminID int64, in PageInput) (*Page, error) {
	p, err := s.pageFromInput(in)
	if err != nil {
		return nil, err
	}
	return s.store.CreatePage(ctx, p)
}

func (s *Service) UpdatePage(ctx context.Context, adminID, id int64, in PageInput) (*Page, error) {
	p, err := s.pageFromInput(in)
	if err != nil {
		return nil, err
	}
	p.ID = id
	return s.store.UpdatePage(ctx, p, adminID)
}

func (s *Service) pageFromInput(in PageInput) (*Page, error) {
	if strings.TrimSpace(in.Title) == "" {
		return nil, ErrInvalid
	}
	slug := Slugify(in.Slug)
	if slug == "" {
		return nil, ErrInvalid
	}
	status := strings.TrimSpace(in.Status)
	if status == "" {
		status = "published"
	}
	switch status {
	case "draft", "published", "archived":
	default:
		return nil, ErrInvalid
	}
	html, _ := markdown.Render(in.BodyMD)
	tmpl := strings.TrimSpace(in.Template)
	if tmpl == "" {
		tmpl = "page"
	}
	return &Page{
		Slug:           slug,
		Title:          strings.TrimSpace(in.Title),
		BodyMD:         in.BodyMD,
		BodyHTML:       html,
		Template:       tmpl,
		ShowInNav:      in.ShowInNav,
		SortOrder:      in.SortOrder,
		Status:         status,
		SEOTitle:       in.SEOTitle,
		SEODescription: in.SEODescription,
	}, nil
}

func (s *Service) PageBySlug(ctx context.Context, slug string) (*Page, error) {
	return s.store.PageBySlug(ctx, slug, true)
}

func (s *Service) PageByID(ctx context.Context, id int64) (*Page, error) {
	return s.store.PageByID(ctx, id)
}

func (s *Service) ListPages(ctx context.Context, includeDeleted bool) ([]Page, error) {
	return s.store.ListPages(ctx, includeDeleted)
}

func (s *Service) DeletePage(ctx context.Context, id int64) error {
	return s.store.SoftDeletePage(ctx, id)
}

func (s *Service) ListTags(ctx context.Context) ([]Tag, error) {
	return s.store.ListTags(ctx)
}

func (s *Service) CreateBanner(ctx context.Context, in BannerInput) (*Banner, error) {
	b, err := bannerFromInput(in)
	if err != nil {
		return nil, err
	}
	return s.store.CreateBanner(ctx, b)
}

func (s *Service) UpdateBanner(ctx context.Context, id int64, in BannerInput) (*Banner, error) {
	b, err := bannerFromInput(in)
	if err != nil {
		return nil, err
	}
	b.ID = id
	return s.store.UpdateBanner(ctx, b)
}

func (s *Service) BannerByID(ctx context.Context, id int64) (*Banner, error) {
	return s.store.BannerByID(ctx, id)
}

func (s *Service) ListBanners(ctx context.Context, enabledOnly bool) ([]Banner, error) {
	return s.store.ListBanners(ctx, enabledOnly)
}

func (s *Service) DeleteBanner(ctx context.Context, id int64) error {
	return s.store.DeleteBanner(ctx, id)
}

func (s *Service) Search(ctx context.Context, q, typ string, limit, offset int) ([]SearchResult, error) {
	return s.store.Search(ctx, q, typ, limit, offset)
}

func (s *Service) RebuildSearch(ctx context.Context) error {
	return s.store.RebuildSearch(ctx)
}

func bannerFromInput(in BannerInput) (*Banner, error) {
	title := strings.TrimSpace(in.Title)
	subtitle := strings.TrimSpace(in.Subtitle)
	link := strings.TrimSpace(in.LinkURL)
	button := strings.TrimSpace(in.ButtonText)
	if title == "" && in.ImageMediaID == 0 {
		return nil, ErrInvalid
	}
	if link != "" {
		if strings.HasPrefix(link, "//") {
			return nil, ErrInvalid
		}
		if !strings.HasPrefix(link, "/") {
			u, err := url.Parse(link)
			if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
				return nil, ErrInvalid
			}
		}
	}
	return &Banner{
		Title:        title,
		Subtitle:     subtitle,
		ImageMediaID: in.ImageMediaID,
		LinkURL:      link,
		ButtonText:   button,
		SortOrder:    in.SortOrder,
		Enabled:      in.Enabled,
	}, nil
}

func (s *Service) TagBySlug(ctx context.Context, slug string) (*Tag, error) {
	return s.store.TagBySlug(ctx, slug)
}

func (s *Service) PublicEntriesByTag(ctx context.Context, tagSlug string, limit, offset int) ([]Entry, error) {
	return s.store.PublicEntriesByTag(ctx, tagSlug, limit, offset)
}

func (s *Service) IncrementEntryView(ctx context.Context, id int64) {
	s.store.IncrementEntryView(ctx, id)
}

func (s *Service) Counts(ctx context.Context) (channels, entries, pages, media int, err error) {
	return s.store.Counts(ctx)
}

func (s *Service) Revisions(ctx context.Context, targetType string, targetID int64) ([]Revision, error) {
	return s.store.Revisions(ctx, targetType, targetID)
}

func (s *Service) RevisionByID(ctx context.Context, id int64) (*Revision, error) {
	return s.store.RevisionByID(ctx, id)
}

// RestoreRevision copies a historical revision back into the entry/page. It
// returns the target type and id for redirecting the admin UI.
func (s *Service) RestoreRevision(ctx context.Context, adminID, revisionID int64) (string, int64, error) {
	rev, err := s.store.RevisionByID(ctx, revisionID)
	if err != nil {
		return "", 0, err
	}
	switch rev.TargetType {
	case "entry":
		e, err := s.store.EntryByID(ctx, rev.TargetID)
		if err != nil {
			return "", 0, err
		}
		e.Title = rev.Title
		e.BodyMD = rev.BodyMD
		e.BodyHTML = rev.BodyHTML
		if _, err := s.store.UpdateEntry(ctx, e, adminID); err != nil {
			return "", 0, err
		}
		return "entry", e.ID, nil
	case "page":
		p, err := s.store.PageByID(ctx, rev.TargetID)
		if err != nil {
			return "", 0, err
		}
		p.Title = rev.Title
		p.BodyMD = rev.BodyMD
		p.BodyHTML = rev.BodyHTML
		if _, err := s.store.UpdatePage(ctx, p, adminID); err != nil {
			return "", 0, err
		}
		return "page", p.ID, nil
	default:
		return "", 0, ErrInvalid
	}
}

func normalizeEntryStatus(status string) (string, error) {
	status = strings.TrimSpace(status)
	if status == "" {
		return "draft", nil
	}
	switch status {
	case "draft", "published", "archived":
		return status, nil
	default:
		return "", ErrInvalid
	}
}

func tagModels(names []string) []Tag {
	out := make([]Tag, 0, len(names))
	seen := map[string]bool{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		slug := Slugify(name)
		if slug == "" || seen[slug] {
			continue
		}
		seen[slug] = true
		out = append(out, Tag{Slug: slug, Name: name})
	}
	return out
}

var slugCleanRE = regexp.MustCompile(`[^a-z0-9]+`)

func Slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) && r < 128 || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	out := slugCleanRE.ReplaceAllString(b.String(), "-")
	return strings.Trim(out, "-")
}
