package content

import "errors"

var (
	ErrNotFound        = errors.New("content: not found")
	ErrConflict        = errors.New("content: conflict")
	ErrInvalid         = errors.New("content: invalid")
	ErrChannelNotEmpty = errors.New("content: channel has entries")
)

type Channel struct {
	ID              int64
	ParentID        int64
	Slug            string
	Name            string
	Type            string
	Description     string
	ListTemplate    string
	DetailTemplate  string
	ShowInNav       bool
	CommentsEnabled bool
	SortOrder       int
	Status          string
	SEOTitle        string
	SEODescription  string
	CreatedAt       int64
	UpdatedAt       int64
}

type Entry struct {
	ID            int64
	ChannelID     int64
	ChannelSlug   string
	ChannelName   string
	ChannelType   string
	Slug          string
	Title         string
	Summary       string
	BodyMD        string
	BodyHTML      string
	CoverMediaID  int64
	AuthorAdminID int64
	AuthorName    string
	Status        string
	Pinned        bool
	Featured      bool
	SortOrder     int
	ViewCount     int64
	ExtraJSON     string
	Extra         map[string]any
	PublishedAt   int64
	CreatedAt     int64
	UpdatedAt     int64
	DeletedAt     int64
	Tags          []Tag
}

type Page struct {
	ID             int64
	Slug           string
	Title          string
	BodyMD         string
	BodyHTML       string
	Template       string
	ShowInNav      bool
	SortOrder      int
	Status         string
	SEOTitle       string
	SEODescription string
	CreatedAt      int64
	UpdatedAt      int64
	DeletedAt      int64
}

type Tag struct {
	ID   int64
	Slug string
	Name string
}

type Revision struct {
	ID            int64
	TargetType    string
	TargetID      int64
	Title         string
	BodyMD        string
	BodyHTML      string
	EditorAdminID int64
	CreatedAt     int64
}

type Media struct {
	ID           int64
	Kind         string
	StoredName   string
	OriginalName string
	MIME         string
	Size         int64
	Width        int
	Height       int
	SHA256       string
	UploaderType string
	UploaderID   int64
	Status       string
	CreatedAt    int64
	Variants     []MediaVariant
}

type MediaVariant struct {
	ID         int64
	MediaID    int64
	Label      string
	StoredName string
	Width      int
	Height     int
	Size       int64
}

type EntryInput struct {
	ChannelID    int64
	Slug         string
	Title        string
	Summary      string
	BodyMD       string
	CoverMediaID int64
	Status       string
	Pinned       bool
	Featured     bool
	SortOrder    int
	ExtraJSON    string
	Tags         []string
}

type PageInput struct {
	Slug           string
	Title          string
	BodyMD         string
	Template       string
	ShowInNav      bool
	SortOrder      int
	Status         string
	SEOTitle       string
	SEODescription string
}

type Banner struct {
	ID           int64
	Title        string
	Subtitle     string
	ImageMediaID int64
	LinkURL      string
	ButtonText   string
	SortOrder    int
	Enabled      bool
	CreatedAt    int64
	UpdatedAt    int64
}

type BannerInput struct {
	Title        string
	Subtitle     string
	ImageMediaID int64
	LinkURL      string
	ButtonText   string
	SortOrder    int
	Enabled      bool
}

type SearchResult struct {
	Type        string
	ID          int64
	Title       string
	Summary     string
	URL         string
	ChannelSlug string
	ChannelName string
	PublishedAt int64
}

type Attachment struct {
	ID            int64
	EntryID       int64
	MediaID       int64
	Title         string
	StoredName    string
	OriginalName  string
	MIME          string
	Size          int64
	SortOrder     int
	DownloadCount int64
}

type Comment struct {
	ID          int64
	EntryID     int64
	UserID      int64
	AuthorName  string
	AuthorEmail string
	BodyMD      string
	BodyHTML    string
	Status      string
	IP          string
	CreatedAt   int64
	UpdatedAt   int64
}

type FriendLink struct {
	ID          int64
	Name        string
	URL         string
	LogoMediaID int64
	SortOrder   int
	Enabled     bool
	CreatedAt   int64
}

type Redirect struct {
	ID         int64
	FromPath   string
	ToPath     string
	StatusCode int
	CreatedAt  int64
}
