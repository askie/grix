package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateReachTemplate(t *testing.T) {
	setupReachTestDB(t)

	tpl, err := CreateReachTemplate(CreateReachTemplateReq{
		Name:      "新版发布通知",
		Title:     "Grix 更新了",
		InAppBody: "我们发布了新版本",
		PushBody:  "Grix 有更新",
		EmailHTML: "<h1>更新</h1>",
	})
	require.NoError(t, err)
	assert.NotZero(t, tpl.ID)
	assert.Equal(t, "新版发布通知", tpl.Name)
}

func TestCreateReachTemplate_MissingName(t *testing.T) {
	setupReachTestDB(t)

	_, err := CreateReachTemplate(CreateReachTemplateReq{Title: "x"})
	assert.Error(t, err, "name is required")

	_, err = CreateReachTemplate(CreateReachTemplateReq{Name: "x"})
	assert.Error(t, err, "title is required")
}

func TestUpdateReachTemplate(t *testing.T) {
	setupReachTestDB(t)

	tpl, _ := CreateReachTemplate(CreateReachTemplateReq{
		Name: "原始", Title: "原始标题",
	})
	newName := "更新后"
	updated, err := UpdateReachTemplate(tpl.ID, UpdateReachTemplateReq{Name: &newName})
	require.NoError(t, err)
	assert.Equal(t, "更新后", updated.Name)
	assert.Equal(t, "原始标题", updated.Title, "unchanged fields preserved")
}

func TestDeleteReachTemplate(t *testing.T) {
	setupReachTestDB(t)

	tpl, _ := CreateReachTemplate(CreateReachTemplateReq{Name: "删我", Title: "x"})
	require.NoError(t, DeleteReachTemplate(tpl.ID))
	_, err := GetReachTemplate(tpl.ID)
	assert.Error(t, err, "deleted template should not be found")
}

func TestListReachTemplates(t *testing.T) {
	setupReachTestDB(t)

	CreateReachTemplate(CreateReachTemplateReq{Name: "a", Title: "ta"})
	CreateReachTemplate(CreateReachTemplateReq{Name: "b", Title: "tb"})

	result, err := ListReachTemplates(ListReachTemplatesReq{Page: 1, PageSize: 10})
	require.NoError(t, err)
	assert.Equal(t, int64(2), result.Total)
	assert.Len(t, result.Templates, 2)
}
