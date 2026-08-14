package service

import (
	"errors"
	"strings"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

var (
	ErrFavoritePathEmpty      = errors.New("path cannot be empty")
	ErrFavoriteNameEmpty      = errors.New("name cannot be empty")
	ErrFavoriteAlreadyExists  = errors.New("path already favorited")
	ErrFavoriteNotFound       = errors.New("favorite not found")
	ErrFavoriteNotOwned       = errors.New("favorite not owned by current user")
)

type UserFavoritePathResp struct {
	ID          int64  `json:"id,string"`
	Path        string `json:"path"`
	Name        string `json:"name"`
	IsDirectory bool   `json:"is_directory"`
	MachineName string `json:"machine_name"`
	CreatedAt   string `json:"created_at"`
}

type AddFavoritePathReq struct {
	Path        string `json:"path" binding:"required"`
	Name        string `json:"name" binding:"required"`
	IsDirectory bool   `json:"is_directory"`
	MachineName string `json:"machine_name"`
}

func ListUserFavoritePaths(userID int64) ([]UserFavoritePathResp, error) {
	var favorites []model.UserFavoritePath
	if err := store.DB.Where("user_id = ?", userID).Order("created_at DESC").Find(&favorites).Error; err != nil {
		return nil, err
	}

	result := make([]UserFavoritePathResp, len(favorites))
	for i, f := range favorites {
		result[i] = UserFavoritePathResp{
			ID:          f.ID,
			Path:        f.Path,
			Name:        f.Name,
			IsDirectory: f.IsDirectory,
			MachineName: f.MachineName,
			CreatedAt:   f.CreatedAt.Format("2006-01-02T15:04:05Z"),
		}
	}
	return result, nil
}

func AddUserFavoritePath(userID int64, req AddFavoritePathReq) (*UserFavoritePathResp, error) {
	if strings.TrimSpace(req.Path) == "" {
		return nil, ErrFavoritePathEmpty
	}
	if strings.TrimSpace(req.Name) == "" {
		return nil, ErrFavoriteNameEmpty
	}

	fav := model.UserFavoritePath{
		ID:          snowflake.GenID(),
		UserID:      userID,
		Path:        req.Path,
		Name:        req.Name,
		IsDirectory: req.IsDirectory,
		MachineName: strings.TrimSpace(req.MachineName),
	}
	if err := store.DB.Create(&fav).Error; err != nil {
		if isDuplicateKeyError(err) {
			return nil, ErrFavoriteAlreadyExists
		}
		return nil, err
	}

	return &UserFavoritePathResp{
		ID:          fav.ID,
		Path:        fav.Path,
		Name:        fav.Name,
		IsDirectory: fav.IsDirectory,
		MachineName: fav.MachineName,
		CreatedAt:   fav.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}, nil
}

func DeleteUserFavoritePath(userID int64, favoriteID int64) error {
	var fav model.UserFavoritePath
	if err := store.DB.Where("id = ?", favoriteID).First(&fav).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrFavoriteNotFound
		}
		return err
	}
	if fav.UserID != userID {
		return ErrFavoriteNotOwned
	}
	return store.DB.Delete(&fav).Error
}

func isDuplicateKeyError(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}
