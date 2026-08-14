package service

import (
	"fmt"
	"strings"
)

func buildEggArtifactObjectKey(eggID, fileName string) string {
	return fmt.Sprintf(
		"eggs/%s/%s",
		safeEggKeySegment(eggID),
		safeEggKeySegment(fileName),
	)
}

// safeEggKeySegment 清洗 egg 制品 key 段，剥离目录穿越成分，非法时回退占位，
// 避免越权写入 eggs/ 之外的对象前缀。
func safeEggKeySegment(s string) string {
	if seg, ok := safeObjectSegment(s); ok {
		return seg
	}
	return "_invalid"
}

func buildEggVersionArtifactObjectKey(eggID string, version int, role string) string {
	return buildEggArtifactObjectKey(
		eggID,
		fmt.Sprintf("%d_%s.zip", version, strings.TrimSpace(role)),
	)
}

func buildAdminEggVersionUploadObjectKey(eggID string, version int, filename string) string {
	return buildEggVersionArtifactObjectKey(eggID, version, resolveEggArtifactRole(filename))
}

func resolveEggArtifactRole(filename string) string {
	normalized := strings.ToLower(strings.TrimSpace(filename))
	if strings.Contains(normalized, "skill") {
		return "skill"
	}
	return "persona"
}
