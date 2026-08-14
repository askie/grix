package service

import (
	"encoding/json"
	"mime/multipart"
)

type EggSearchReq struct {
	Keyword    string
	CategoryID string
	Locale     string
	Page       int
	PageSize   int
}

type EggCategoryListReq struct {
	Locale string
}

type EggCategoryListItem struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type EggCategoryListResp struct {
	LocaleUsed string                `json:"locale_used"`
	List       []EggCategoryListItem `json:"list"`
}

type EggSearchItem struct {
	ID                       string   `json:"id"`
	CategoryID               string   `json:"category_id"`
	CategoryName             string   `json:"category_name"`
	Name                     string   `json:"name"`
	Description              string   `json:"description"`
	Color                    string   `json:"color"`
	Emoji                    string   `json:"emoji"`
	Vibe                     string   `json:"vibe"`
	CanCreateAgent           bool     `json:"can_create_agent"`
	ExistingAgentClientTypes []string `json:"existing_agent_client_types"`
	Status                   string   `json:"status"`
	Version                  int      `json:"version"`
	VersionDesc              string   `json:"version_desc"`
	InstallCount             int64    `json:"install_count"`
}

type EggSearchResp struct {
	LocaleUsed string          `json:"locale_used"`
	Page       int             `json:"page"`
	PageSize   int             `json:"page_size"`
	HasMore    bool            `json:"has_more"`
	List       []EggSearchItem `json:"list"`
}

type EggGetReq struct {
	ID      string
	Locale  string
	Version int
}

type EggGetResp struct {
	LocaleUsed               string          `json:"locale_used"`
	ID                       string          `json:"id"`
	CategoryID               string          `json:"category_id"`
	CategoryName             string          `json:"category_name"`
	Name                     string          `json:"name"`
	Description              string          `json:"description"`
	Color                    string          `json:"color"`
	Emoji                    string          `json:"emoji"`
	Vibe                     string          `json:"vibe"`
	CanCreateAgent           bool            `json:"can_create_agent"`
	ExistingAgentClientTypes []string        `json:"existing_agent_client_types"`
	Status                   string          `json:"status"`
	Version                  int             `json:"version"`
	VersionDesc              string          `json:"version_desc"`
	InstallCount             int64           `json:"install_count"`
	ArtifactManifest         json.RawMessage `json:"artifact_manifest"`
}

type EggInstallReq struct {
	EggID           string `json:"egg_id"`
	Version         int    `json:"version"`
	IdempotencyKey  string `json:"idempotency_key"`
	InstallMode     string `json:"install_mode"`
	TargetAgentID   *int64 `json:"target_agent_id,string"`
	ExecutorAgentID *int64 `json:"executor_agent_id,string"`
	Locale          string `json:"-"`
}

type EggInstallCandidateAgent struct {
	AgentID         string `json:"agent_id"`
	AgentName       string `json:"agent_name"`
	AgentClientType string `json:"agent_client_type"`
}

type EggInstallAcceptResp struct {
	InstallID       string                     `json:"install_id"`
	Status          string                     `json:"status"`
	SessionID       string                     `json:"session_id"`
	ExecutorAgentID string                     `json:"executor_agent_id"`
	Candidates      []EggInstallCandidateAgent `json:"candidates,omitempty"`
}

type EggInstallStatusResp struct {
	InstallID       string `json:"install_id"`
	Status          string `json:"status"`
	Step            string `json:"step"`
	SessionID       string `json:"session_id"`
	ExecutorAgentID string `json:"executor_agent_id"`
	TargetAgentID   string `json:"target_agent_id"`
	ErrorCode       string `json:"error_code"`
	ErrorMsg        string `json:"error_msg"`
}

type EggCategoryI18nInput struct {
	Locale      string `json:"locale"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type EggCategoryResp struct {
	ID        string                  `json:"id"`
	Code      string                  `json:"code"`
	Status    string                  `json:"status"`
	SortOrder int                     `json:"sort_order"`
	I18n      []EggCategoryI18nOutput `json:"i18n"`
}

type EggCategoryI18nOutput struct {
	Locale      string `json:"locale"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type AdminEggCategoryListReq struct {
	Status  string
	Keyword string
}

type AdminEggCategoryCreateReq struct {
	ID        string                 `json:"id"`
	Code      string                 `json:"code"`
	Status    string                 `json:"status"`
	SortOrder int                    `json:"sort_order"`
	I18n      []EggCategoryI18nInput `json:"i18n"`
}

type AdminEggCategoryUpdateReq struct {
	Code      string                 `json:"code"`
	SortOrder int                    `json:"sort_order"`
	I18n      []EggCategoryI18nInput `json:"i18n"`
}

type AdminEggCategoryStatusReq struct {
	Status string `json:"status"`
}

type EggI18nInput struct {
	Locale      string `json:"locale"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Vibe        string `json:"vibe"`
}

type EggI18nOutput struct {
	Locale      string `json:"locale"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Vibe        string `json:"vibe"`
}

type AdminEggListReq struct {
	Status     string
	CategoryID string
	Keyword    string
	Page       int
	PageSize   int
}

type AdminEggListItem struct {
	ID           string `json:"id"`
	CategoryID   string `json:"category_id"`
	Status       string `json:"status"`
	InstallCount int64  `json:"install_count"`
	Pinned       bool   `json:"pinned"`
	PinnedAt     int64  `json:"pinned_at,omitempty"`
	UpdatedAt    int64  `json:"updated_at"`
}

type AdminEggListResp struct {
	Page     int                `json:"page"`
	PageSize int                `json:"page_size"`
	Total    int64              `json:"total"`
	HasMore  bool               `json:"has_more"`
	List     []AdminEggListItem `json:"list"`
}

type AdminEggDetailResp struct {
	ID           string          `json:"id"`
	CategoryID   string          `json:"category_id"`
	Color        string          `json:"color"`
	Emoji        string          `json:"emoji"`
	Status       string          `json:"status"`
	InstallCount int64           `json:"install_count"`
	Pinned       bool            `json:"pinned"`
	PinnedAt     int64           `json:"pinned_at,omitempty"`
	I18n         []EggI18nOutput `json:"i18n"`
}

type AdminEggCreateReq struct {
	ID         string         `json:"id"`
	CategoryID string         `json:"category_id"`
	Color      string         `json:"color"`
	Emoji      string         `json:"emoji"`
	I18n       []EggI18nInput `json:"i18n"`
}

type AdminEggUpdateReq struct {
	CategoryID string         `json:"category_id"`
	Color      string         `json:"color"`
	Emoji      string         `json:"emoji"`
	I18n       []EggI18nInput `json:"i18n"`
}

type AdminEggStatusReq struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

type AdminEggPinReq struct {
	Pinned bool `json:"pinned"`
}

type EggVersionI18nInput struct {
	Locale      string `json:"locale"`
	VersionDesc string `json:"version_desc"`
}

type EggVersionResp struct {
	EggID            string                `json:"egg_id"`
	Version          int                   `json:"version"`
	ZipURL           string                `json:"zip_url"`    // legacy
	ZipSHA256        string                `json:"zip_sha256"` // legacy
	ZipSize          int64                 `json:"zip_size"`   // legacy
	PersonaZipURL    string                `json:"persona_zip_url,omitempty"`
	PersonaZipSHA256 string                `json:"persona_zip_sha256,omitempty"`
	PersonaZipSize   int64                 `json:"persona_zip_size,omitempty"`
	SkillZipURL      string                `json:"skill_zip_url,omitempty"`
	SkillZipSHA256   string                `json:"skill_zip_sha256,omitempty"`
	SkillZipSize     int64                 `json:"skill_zip_size,omitempty"`
	ArtifactManifest json.RawMessage       `json:"artifact_manifest"`
	PublishedAt      int64                 `json:"published_at"`
	I18n             []EggVersionI18nInput `json:"i18n"`
}

type AdminEggVersionPresignReq struct {
	Filename string `json:"filename"`
}

type AdminEggVersionPresignResp struct {
	UploadURL string `json:"upload_url"`
	ObjectKey string `json:"object_key"`
	ZipURL    string `json:"zip_url"`
}

type AdminEggVersionCreateReq struct {
	Version          int                   `json:"version"`
	ZipURL           string                `json:"zip_url"`    // legacy
	ZipSHA256        string                `json:"zip_sha256"` // legacy
	ZipSize          int64                 `json:"zip_size"`   // legacy
	PersonaZipURL    string                `json:"persona_zip_url"`
	PersonaZipSHA256 string                `json:"persona_zip_sha256"`
	PersonaZipSize   int64                 `json:"persona_zip_size"`
	SkillZipURL      string                `json:"skill_zip_url"`
	SkillZipSHA256   string                `json:"skill_zip_sha256"`
	SkillZipSize     int64                 `json:"skill_zip_size"`
	ArtifactManifest json.RawMessage       `json:"artifact_manifest"`
	I18n             []EggVersionI18nInput `json:"i18n"`
}

type AdminEggVersionUpdateReq struct {
	ArtifactManifest json.RawMessage       `json:"artifact_manifest"`
	I18n             []EggVersionI18nInput `json:"i18n"`
}

type AdminEggCreateWithFilesMeta struct {
	ID               string                `json:"id"`
	CategoryID       string                `json:"category_id"`
	Color            string                `json:"color"`
	Emoji            string                `json:"emoji"`
	PublishNow       *bool                 `json:"publish_now"`
	EggI18n          []EggI18nInput        `json:"egg_i18n"`
	VersionI18n      []EggVersionI18nInput `json:"version_i18n"`
	ArtifactManifest json.RawMessage       `json:"artifact_manifest"`
}

type AdminEggCreateWithFilesReq struct {
	Meta           AdminEggCreateWithFilesMeta
	PersonaZipFile *multipart.FileHeader
	SkillZipFile   *multipart.FileHeader
}

type AdminEggCreateWithFilesResp struct {
	EggID            string `json:"egg_id"`
	Version          int    `json:"version"`
	Status           string `json:"status"`
	Created          bool   `json:"created"`
	PersonaZipURL    string `json:"persona_zip_url,omitempty"`
	PersonaZipSHA256 string `json:"persona_zip_sha256,omitempty"`
	PersonaZipSize   int64  `json:"persona_zip_size,omitempty"`
	SkillZipURL      string `json:"skill_zip_url,omitempty"`
	SkillZipSHA256   string `json:"skill_zip_sha256,omitempty"`
	SkillZipSize     int64  `json:"skill_zip_size,omitempty"`
}
