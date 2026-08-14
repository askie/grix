package service

import (
	"errors"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
)

type AgentCategoryReq struct {
	ParentID  int64  `json:"parent_id,string"`
	Name      string `json:"name" binding:"required"`
	SortOrder int    `json:"sort_order"`
}

type AgentCategoryResp struct {
	ID        int64  `json:"id,string"`
	ParentID  int64  `json:"parent_id,string"`
	Name      string `json:"name"`
	SortOrder int    `json:"sort_order"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

const maxAgentCategoryNameRunes = 50

func categoryToResp(c *model.AgentCategory) AgentCategoryResp {
	return AgentCategoryResp{
		ID:        c.ID,
		ParentID:  c.ParentID,
		Name:      c.Name,
		SortOrder: c.SortOrder,
		CreatedAt: c.CreatedAt.Unix(),
		UpdatedAt: c.UpdatedAt.Unix(),
	}
}

func validateCategoryName(name string) (string, *errcode.ErrCode) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", &errcode.ErrCode{
			HTTPStatus: 400,
			BizCode:    10003,
			Msg:        "分类名称不能为空",
		}
	}
	if len([]rune(name)) > maxAgentCategoryNameRunes {
		return "", &errcode.ErrCode{
			HTTPStatus: 400,
			BizCode:    10003,
			Msg:        "分类名称过长",
		}
	}
	return name, nil
}

// isDescendant checks if candidateParentID is a descendant of categoryID,
// preventing circular parent references in the category tree.
func isDescendant(ownerID, categoryID, candidateParentID int64) bool {
	visited := make(map[int64]bool)
	currentID := candidateParentID
	for currentID != 0 {
		if currentID == categoryID {
			return true
		}
		if visited[currentID] {
			return true // already a cycle in the data
		}
		visited[currentID] = true
		var parent model.AgentCategory
		if err := store.DB.Select("id", "parent_id").Where("id = ? AND owner_id = ?", currentID, ownerID).First(&parent).Error; err != nil {
			return false
		}
		currentID = parent.ParentID
	}
	return false
}

// CheckOwnerCategory checks if the category exists and belongs to the user. ParentID 0 is always valid.
func CheckOwnerCategory(ownerID, categoryID int64) *errcode.ErrCode {
	if categoryID == 0 {
		return nil
	}
	var count int64
	store.DB.Model(&model.AgentCategory{}).Where("id = ? AND owner_id = ?", categoryID, ownerID).Count(&count)
	if count == 0 {
		return &errcode.ErrCode{
			HTTPStatus: 403,
			BizCode:    10002,
			Msg:        "无效的分类或无权访问",
		}
	}
	return nil
}

func AgentCategoryList(ownerID int64) ([]AgentCategoryResp, *errcode.ErrCode) {
	var categories []model.AgentCategory
	if err := store.DB.Where("owner_id = ?", ownerID).Order("sort_order asc, id asc").Find(&categories).Error; err != nil {
		return nil, &errcode.ErrCode{
			HTTPStatus: 500,
			BizCode:    50001,
			Msg:        "拉取分类失败",
		}
	}
	resp := make([]AgentCategoryResp, len(categories))
	for i, c := range categories {
		resp[i] = categoryToResp(&c)
	}
	return resp, nil
}

func AgentCategoryCreate(ownerID int64, req AgentCategoryReq) (*AgentCategoryResp, *errcode.ErrCode) {
	name, ec := validateCategoryName(req.Name)
	if ec != nil {
		return nil, ec
	}
	if ec := CheckOwnerCategory(ownerID, req.ParentID); ec != nil {
		return nil, ec
	}

	cat := model.AgentCategory{
		OwnerID:   ownerID,
		ParentID:  req.ParentID,
		Name:      name,
		SortOrder: req.SortOrder,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := store.DB.Create(&cat).Error; err != nil {
		return nil, &errcode.ErrCode{
			HTTPStatus: 500,
			BizCode:    50001,
			Msg:        "创建分类失败",
		}
	}
	resp := categoryToResp(&cat)
	return &resp, nil
}

func AgentCategoryUpdate(ownerID, categoryID int64, req AgentCategoryReq) (*AgentCategoryResp, *errcode.ErrCode) {
	name, ec := validateCategoryName(req.Name)
	if ec != nil {
		return nil, ec
	}

	var cat model.AgentCategory
	if err := store.DB.Where("id = ? AND owner_id = ?", categoryID, ownerID).First(&cat).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, &errcode.ErrCode{
				HTTPStatus: 404,
				BizCode:    10002,
				Msg:        "分类不存在或无权访问",
			}
		}
		return nil, &errcode.ErrCode{
			HTTPStatus: 500,
			BizCode:    50001,
			Msg:        "查询分类失败",
		}
	}

	if req.ParentID != cat.ParentID {
		if req.ParentID == categoryID {
			return nil, &errcode.ErrCode{
				HTTPStatus: 400,
				BizCode:    10003,
				Msg:        "不能将父级分类设置为自己",
			}
		}
		if ec := CheckOwnerCategory(ownerID, req.ParentID); ec != nil {
			return nil, ec
		}
		// 检测深层循环引用：确保新的 parent 不是自己的后代节点
		if isDescendant(ownerID, categoryID, req.ParentID) {
			return nil, &errcode.ErrCode{
				HTTPStatus: 400,
				BizCode:    10003,
				Msg:        "不能将父级分类设置为自己的子分类，会产生循环引用",
			}
		}
	}

	cat.ParentID = req.ParentID
	cat.Name = name
	cat.SortOrder = req.SortOrder
	cat.UpdatedAt = time.Now()

	if err := store.DB.Save(&cat).Error; err != nil {
		return nil, &errcode.ErrCode{
			HTTPStatus: 500,
			BizCode:    50001,
			Msg:        "更新分类失败",
		}
	}
	resp := categoryToResp(&cat)
	return &resp, nil
}

// AgentCategoryDelete deletes the category if it contains no agents and no subcategories.
func AgentCategoryDelete(ownerID, categoryID int64) *errcode.ErrCode {
	var cat model.AgentCategory
	if err := store.DB.Where("id = ? AND owner_id = ?", categoryID, ownerID).First(&cat).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &errcode.ErrCode{
				HTTPStatus: 404,
				BizCode:    10002,
				Msg:        "分类不存在或无权访问",
			}
		}
		return &errcode.ErrCode{
			HTTPStatus: 500,
			BizCode:    50001,
			Msg:        "查询分类失败",
		}
	}

	var childCount int64
	store.DB.Model(&model.AgentCategory{}).Where("parent_id = ?", categoryID).Count(&childCount)
	if childCount > 0 {
		return &errcode.ErrCode{
			HTTPStatus: 400,
			BizCode:    10003,
			Msg:        "该目录下仍有子分类，无法直接删除",
		}
	}

	var agentCount int64
	store.DB.Model(&model.Agent{}).Where("category_id = ? AND status != 3", categoryID).Count(&agentCount)
	if agentCount > 0 {
		return &errcode.ErrCode{
			HTTPStatus: 400,
			BizCode:    10003,
			Msg:        "该目录下仍有智能体，无法直接删除",
		}
	}

	if err := store.DB.Delete(&cat).Error; err != nil {
		return &errcode.ErrCode{
			HTTPStatus: 500,
			BizCode:    50001,
			Msg:        "删除分类失败",
		}
	}
	return nil
}
