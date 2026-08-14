package service

import (
	"bytes"
	"context"
	"errors"
	"image"
	stddraw "image/draw"
	"image/jpeg"
	"io"
	"log"
	"mime/multipart"
	"strings"

	"github.com/askie/grix/backend/config"
	"github.com/minio/minio-go/v7"
	xdraw "golang.org/x/image/draw"

	_ "image/gif"
	_ "image/png"

	_ "golang.org/x/image/webp"
)

const (
	userAvatarMaxUploadBytes = 10 * 1024 * 1024
	userAvatarTargetSizePx   = 512
	userAvatarJPEGQuality    = 85
)

var (
	ErrAvatarFileRequired = errors.New("avatar file is required")
	ErrAvatarFileTooLarge = errors.New("avatar file too large")
	ErrAvatarImageInvalid = errors.New("avatar image is invalid")
)

func UploadUserAvatar(userID int64, fileHeader *multipart.FileHeader) (string, error) {
	if fileHeader == nil {
		return "", ErrAvatarFileRequired
	}
	if err := ensureAvatarOSSReady(); err != nil {
		return "", err
	}
	if fileHeader.Size <= 0 {
		return "", ErrAvatarFileRequired
	}
	if fileHeader.Size > userAvatarMaxUploadBytes {
		return "", ErrAvatarFileTooLarge
	}
	previousAvatarURL, err := getUserAvatarURL(userID)
	if err != nil {
		return "", err
	}

	file, err := fileHeader.Open()
	if err != nil {
		return "", err
	}
	defer file.Close()

	raw, err := readAvatarUploadBytes(file, userAvatarMaxUploadBytes+1)
	if err != nil {
		return "", err
	}
	if int64(len(raw)) > userAvatarMaxUploadBytes {
		return "", ErrAvatarFileTooLarge
	}

	jpegBytes, err := normalizeAvatarImage(raw)
	if err != nil {
		return "", err
	}

	objectKey := buildUserAvatarObjectKey(userID)
	_, err = getOSSClient(ossStorageAvatar).PutObject(
		context.Background(),
		config.C.OSS.Avatar.Bucket,
		objectKey,
		bytes.NewReader(jpegBytes),
		int64(len(jpegBytes)),
		minio.PutObjectOptions{ContentType: "image/jpeg"},
	)
	if err != nil {
		return "", err
	}

	avatarURL := buildAvatarAccessURL(objectKey)
	if err := UpdateUserProfile(userID, nil, &avatarURL, nil); err != nil {
		return "", err
	}
	if err := deletePreviousAvatarObject(userID, previousAvatarURL, objectKey); err != nil {
		log.Printf(
			"UploadUserAvatar cleanup old avatar failed: user_id=%d old_avatar_url=%q error=%v",
			userID,
			previousAvatarURL,
			err,
		)
	}
	return avatarURL, nil
}

func getUserAvatarURL(userID int64) (string, error) {
	user, err := GetUserProfile(userID)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(user.AvatarURL), nil
}

func deletePreviousAvatarObject(userID int64, previousAvatarURL, currentObjectKey string) error {
	previousObjectKey := resolveAvatarObjectKey(previousAvatarURL)
	if previousObjectKey == "" || previousObjectKey == currentObjectKey {
		return nil
	}
	if !isUserAvatarObjectKey(userID, previousObjectKey) {
		return nil
	}
	return getOSSClient(ossStorageAvatar).RemoveObject(
		context.Background(),
		config.C.OSS.Avatar.Bucket,
		previousObjectKey,
		minio.RemoveObjectOptions{},
	)
}

func readAvatarUploadBytes(reader io.Reader, limit int64) ([]byte, error) {
	if reader == nil {
		return nil, ErrAvatarFileRequired
	}
	if limit <= 0 {
		return nil, ErrAvatarFileTooLarge
	}
	return io.ReadAll(io.LimitReader(reader, limit))
}

func normalizeAvatarImage(raw []byte) ([]byte, error) {
	if len(raw) == 0 {
		return nil, ErrAvatarImageInvalid
	}
	decoded, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, ErrAvatarImageInvalid
	}

	square := cropCenterSquare(decoded)
	dst := image.NewRGBA(image.Rect(0, 0, userAvatarTargetSizePx, userAvatarTargetSizePx))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), square, square.Bounds(), xdraw.Over, nil)

	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: userAvatarJPEGQuality}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func cropCenterSquare(src image.Image) image.Image {
	bounds := src.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	side := width
	if height < side {
		side = height
	}

	startX := bounds.Min.X + (width-side)/2
	startY := bounds.Min.Y + (height-side)/2

	dst := image.NewRGBA(image.Rect(0, 0, side, side))
	stddraw.Draw(dst, dst.Bounds(), src, image.Point{X: startX, Y: startY}, stddraw.Src)
	return dst
}
