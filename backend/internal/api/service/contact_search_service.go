package service

import (
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
)

const (
	contactSearchDefaultLimit       = 20
	contactPeerTypeUser       int16 = 1
	contactPeerTypeAgent      int16 = 2
)

type ContactSearchItem struct {
	PeerID       int64     `json:"peer_id,string"`
	PeerType     int16     `json:"peer_type"`
	DisplayName  string    `json:"display_name"`
	Introduction string    `json:"introduction"`
	Nickname     string    `json:"-"`
	Username     string    `json:"username"`
	RemarkName   string    `json:"remark_name"`
	AvatarURL    string    `json:"avatar_url"`
	CreatedAt    time.Time `json:"created_at"`
}

type ContactSearchResp struct {
	HasMore bool                `json:"has_more"`
	List    []ContactSearchItem `json:"list"`
}

type friendContactSearchRow struct {
	FriendID     int64     `gorm:"column:friend_id"`
	Username     string    `gorm:"column:username"`
	Nickname     string    `gorm:"column:nickname"`
	Introduction string    `gorm:"column:introduction"`
	RemarkName   string    `gorm:"column:remark_name"`
	AvatarURL    string    `gorm:"column:avatar_url"`
	CreatedAt    time.Time `gorm:"column:created_at"`
}

type agentContactSearchRow struct {
	AgentID      int64     `gorm:"column:agent_id"`
	AgentName    string    `gorm:"column:agent_name"`
	Introduction string    `gorm:"column:introduction"`
	AvatarURL    string    `gorm:"column:avatar_url"`
	CreatedAt    time.Time `gorm:"column:created_at"`
}

type contactSearchKeyword struct {
	lowered string
	compact string
	tokens  []string
	numeric string
}

type rankedContactSearchItem struct {
	item  ContactSearchItem
	score int
}

func ContactListAll(userID int64, limit, offset int) (*ContactSearchResp, error) {
	if limit <= 0 {
		limit = contactSearchDefaultLimit
	}
	if offset < 0 {
		offset = 0
	}

	// Fetch limit+offset+1 from each table so we can merge-sort and still
	// have enough rows for [offset, offset+limit+1] after combining.
	fetchLimit := limit + offset + 1

	friendItems, err := listAllFriendContacts(userID, fetchLimit)
	if err != nil {
		return nil, err
	}
	agentItems, err := listAllAgentContacts(userID, fetchLimit)
	if err != nil {
		return nil, err
	}

	return buildContactSearchResp(sortContactSearchItems(append(friendItems, agentItems...)), limit, offset), nil
}

func listAllFriendContacts(userID int64, fetchLimit int) ([]ContactSearchItem, error) {
	query := store.DB.Table("friends AS f").
		Select("f.friend_id, u.username, u.nickname, u.introduction, f.remark_name, u.avatar_url, f.created_at").
		Joins("JOIN users u ON u.id = f.friend_id").
		Where("f.user_id = ?", userID)
	query = applyContactSearchVisibilityFilter(query)

	var rows []friendContactSearchRow
	if err := query.Order("f.created_at DESC").Limit(fetchLimit).Scan(&rows).Error; err != nil {
		return nil, err
	}

	items := make([]ContactSearchItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, ContactSearchItem{
			PeerID:       row.FriendID,
			PeerType:     contactPeerTypeUser,
			DisplayName:  resolveFriendDisplayNickname(row.RemarkName, row.Nickname, row.Username),
			Introduction: strings.TrimSpace(row.Introduction),
			Nickname:     row.Nickname,
			Username:     row.Username,
			RemarkName:   strings.TrimSpace(row.RemarkName),
			AvatarURL:    row.AvatarURL,
			CreatedAt:    row.CreatedAt,
		})
	}
	return items, nil
}

func listAllAgentContacts(userID int64, fetchLimit int) ([]ContactSearchItem, error) {
	var rows []agentContactSearchRow
	if err := store.DB.Table("agents").
		Select("id AS agent_id, agent_name, introduction, avatar_url, created_at").
		Where("owner_id = ? AND status = ?", userID, model.AgentStatusActive).
		Order("created_at DESC").
		Limit(fetchLimit).
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	items := make([]ContactSearchItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, ContactSearchItem{
			PeerID:       row.AgentID,
			PeerType:     contactPeerTypeAgent,
			DisplayName:  strings.TrimSpace(row.AgentName),
			Introduction: strings.TrimSpace(row.Introduction),
			AvatarURL:    row.AvatarURL,
			CreatedAt:    row.CreatedAt,
		})
	}
	return items, nil
}

func ContactSearch(userID int64, keyword string, limit, offset int) (*ContactSearchResp, error) {
	searchKeyword := buildContactSearchKeyword(keyword)
	if searchKeyword.lowered == "" {
		return &ContactSearchResp{List: []ContactSearchItem{}}, nil
	}

	friendItems, err := searchFriendContacts(userID, searchKeyword)
	if err != nil {
		return nil, err
	}
	agentItems, err := searchAgentContacts(userID, searchKeyword)
	if err != nil {
		return nil, err
	}

	return buildContactSearchResp(rankContactSearchItems(append(friendItems, agentItems...), searchKeyword), limit, offset), nil
}

func ContactSearchByID(userID int64, id int64, limit, offset int) (*ContactSearchResp, error) {
	if id <= 0 {
		return &ContactSearchResp{List: []ContactSearchItem{}}, nil
	}

	friendItems, err := searchFriendContactByID(userID, id)
	if err != nil {
		return nil, err
	}
	agentItems, err := searchAgentContactByID(userID, id)
	if err != nil {
		return nil, err
	}

	return buildContactSearchResp(sortContactSearchItems(append(friendItems, agentItems...)), limit, offset), nil
}

func buildContactSearchResp(items []ContactSearchItem, limit, offset int) *ContactSearchResp {
	if limit <= 0 {
		limit = contactSearchDefaultLimit
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= len(items) {
		return &ContactSearchResp{List: []ContactSearchItem{}}
	}

	end := offset + limit
	hasMore := end < len(items)
	if end > len(items) {
		end = len(items)
	}

	return &ContactSearchResp{
		HasMore: hasMore,
		List:    items[offset:end],
	}
}

func searchFriendContacts(userID int64, keyword contactSearchKeyword) ([]ContactSearchItem, error) {
	query := store.DB.Table("friends AS f").
		Select("f.friend_id, u.username, u.nickname, u.introduction, f.remark_name, u.avatar_url, f.created_at").
		Joins("JOIN users u ON u.id = f.friend_id").
		Where("f.user_id = ?", userID)
	clause, args := buildContactSearchFilter(
		[]string{"f.remark_name", "u.nickname", "u.username"},
		"f.friend_id",
		keyword,
	)
	query = query.Where(clause, args...)
	query = applyContactSearchVisibilityFilter(query)

	var rows []friendContactSearchRow
	if err := query.Order("f.created_at DESC").Scan(&rows).Error; err != nil {
		return nil, err
	}

	items := make([]ContactSearchItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, ContactSearchItem{
			PeerID:       row.FriendID,
			PeerType:     contactPeerTypeUser,
			DisplayName:  resolveFriendDisplayNickname(row.RemarkName, row.Nickname, row.Username),
			Introduction: strings.TrimSpace(row.Introduction),
			Nickname:     row.Nickname,
			Username:     row.Username,
			RemarkName:   strings.TrimSpace(row.RemarkName),
			AvatarURL:    row.AvatarURL,
			CreatedAt:    row.CreatedAt,
		})
	}
	return items, nil
}

func searchFriendContactByID(userID int64, friendID int64) ([]ContactSearchItem, error) {
	query := store.DB.Table("friends AS f").
		Select("f.friend_id, u.username, u.nickname, u.introduction, f.remark_name, u.avatar_url, f.created_at").
		Joins("JOIN users u ON u.id = f.friend_id").
		Where("f.user_id = ? AND f.friend_id = ?", userID, friendID)
	query = applyContactSearchVisibilityFilter(query)

	var rows []friendContactSearchRow
	if err := query.Order("f.created_at DESC").Scan(&rows).Error; err != nil {
		return nil, err
	}

	items := make([]ContactSearchItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, ContactSearchItem{
			PeerID:       row.FriendID,
			PeerType:     contactPeerTypeUser,
			DisplayName:  resolveFriendDisplayNickname(row.RemarkName, row.Nickname, row.Username),
			Introduction: strings.TrimSpace(row.Introduction),
			Nickname:     row.Nickname,
			Username:     row.Username,
			RemarkName:   strings.TrimSpace(row.RemarkName),
			AvatarURL:    row.AvatarURL,
			CreatedAt:    row.CreatedAt,
		})
	}
	return items, nil
}

func searchAgentContacts(userID int64, keyword contactSearchKeyword) ([]ContactSearchItem, error) {
	clause, args := buildContactSearchFilter([]string{"agent_name"}, "id", keyword)
	var rows []agentContactSearchRow
	if err := store.DB.Table("agents").
		Select("id AS agent_id, agent_name, introduction, avatar_url, created_at").
		Where("owner_id = ? AND status = ?", userID, model.AgentStatusActive).
		Where(clause, args...).
		Order("created_at DESC").
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	items := make([]ContactSearchItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, ContactSearchItem{
			PeerID:       row.AgentID,
			PeerType:     contactPeerTypeAgent,
			DisplayName:  strings.TrimSpace(row.AgentName),
			Introduction: strings.TrimSpace(row.Introduction),
			AvatarURL:    row.AvatarURL,
			CreatedAt:    row.CreatedAt,
		})
	}
	return items, nil
}

func searchAgentContactByID(userID int64, agentID int64) ([]ContactSearchItem, error) {
	var rows []agentContactSearchRow
	if err := store.DB.Table("agents").
		Select("id AS agent_id, agent_name, introduction, avatar_url, created_at").
		Where("owner_id = ? AND id = ? AND status = ?", userID, agentID, model.AgentStatusActive).
		Order("created_at DESC").
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	items := make([]ContactSearchItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, ContactSearchItem{
			PeerID:       row.AgentID,
			PeerType:     contactPeerTypeAgent,
			DisplayName:  strings.TrimSpace(row.AgentName),
			Introduction: strings.TrimSpace(row.Introduction),
			AvatarURL:    row.AvatarURL,
			CreatedAt:    row.CreatedAt,
		})
	}
	return items, nil
}

func applyContactSearchVisibilityFilter(query *gorm.DB) *gorm.DB {
	for _, prefix := range hiddenFriendSearchUsernamePrefixes {
		query = query.Where("LOWER(u.username) NOT LIKE ?", prefix+"%")
	}
	return query
}

func buildContactSearchKeyword(keyword string) contactSearchKeyword {
	lowered := strings.ToLower(strings.TrimSpace(keyword))
	if lowered == "" {
		return contactSearchKeyword{}
	}

	tokens := splitSearchTokens(lowered)
	compact := compactSearchText(lowered)
	numeric := ""
	if isDigitsOnly(lowered) {
		numeric = lowered
	}

	return contactSearchKeyword{
		lowered: lowered,
		compact: compact,
		tokens:  tokens,
		numeric: numeric,
	}
}

func splitSearchTokens(text string) []string {
	rawTokens := strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	if len(rawTokens) == 0 {
		return nil
	}

	tokens := make([]string, 0, len(rawTokens))
	seen := make(map[string]struct{}, len(rawTokens))
	for _, token := range rawTokens {
		normalized := strings.TrimSpace(token)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		tokens = append(tokens, normalized)
	}
	return tokens
}

func compactSearchText(text string) string {
	if text == "" {
		return ""
	}
	var builder strings.Builder
	builder.Grow(len(text))
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func isDigitsOnly(text string) bool {
	if text == "" {
		return false
	}
	for _, r := range text {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func buildContactSearchFilter(textFields []string, idColumn string, keyword contactSearchKeyword) (string, []any) {
	var clauses []string
	var args []any

	if clause, clauseArgs := buildRawPhraseClause(textFields, idColumn, keyword); clause != "" {
		clauses = append(clauses, clause)
		args = append(args, clauseArgs...)
	}
	if clause, clauseArgs := buildCompactPhraseClause(textFields, keyword); clause != "" {
		clauses = append(clauses, clause)
		args = append(args, clauseArgs...)
	}
	if clause, clauseArgs := buildTokenClause(textFields, idColumn, keyword); clause != "" {
		clauses = append(clauses, clause)
		args = append(args, clauseArgs...)
	}
	if clause, clauseArgs := buildNumericIDClause(idColumn, keyword.numeric); clause != "" {
		clauses = append(clauses, clause)
		args = append(args, clauseArgs...)
	}

	if len(clauses) == 0 {
		return "1 = 0", nil
	}
	return "(" + strings.Join(clauses, " OR ") + ")", args
}

func buildRawPhraseClause(textFields []string, idColumn string, keyword contactSearchKeyword) (string, []any) {
	if keyword.lowered == "" {
		return "", nil
	}
	pattern := "%" + keyword.lowered + "%"
	var parts []string
	args := make([]any, 0, len(textFields))
	for _, field := range textFields {
		parts = append(parts, "LOWER("+field+") LIKE ?")
		args = append(args, pattern)
	}
	if keyword.numeric != "" {
		parts = append(parts, contactSearchIDExpression(idColumn)+" LIKE ?")
		args = append(args, keyword.numeric+"%")
	}
	return "(" + strings.Join(parts, " OR ") + ")", args
}

func buildCompactPhraseClause(textFields []string, keyword contactSearchKeyword) (string, []any) {
	if keyword.compact == "" {
		return "", nil
	}
	pattern := "%" + keyword.compact + "%"
	var parts []string
	args := make([]any, 0, len(textFields))
	for _, field := range textFields {
		parts = append(parts, contactSearchCompactExpression(field)+" LIKE ?")
		args = append(args, pattern)
	}
	return "(" + strings.Join(parts, " OR ") + ")", args
}

func buildTokenClause(textFields []string, idColumn string, keyword contactSearchKeyword) (string, []any) {
	if len(keyword.tokens) <= 1 {
		return "", nil
	}

	var tokenGroups []string
	args := make([]any, 0, len(keyword.tokens)*len(textFields))
	for _, token := range keyword.tokens {
		pattern := "%" + token + "%"
		var fieldParts []string
		for _, field := range textFields {
			fieldParts = append(fieldParts, "LOWER("+field+") LIKE ?")
			args = append(args, pattern)
		}
		if isDigitsOnly(token) {
			fieldParts = append(fieldParts, contactSearchIDExpression(idColumn)+" LIKE ?")
			args = append(args, token+"%")
		}
		tokenGroups = append(tokenGroups, "("+strings.Join(fieldParts, " OR ")+")")
	}

	return "(" + strings.Join(tokenGroups, " AND ") + ")", args
}

func buildNumericIDClause(idColumn string, numeric string) (string, []any) {
	if numeric == "" {
		return "", nil
	}
	return contactSearchIDExpression(idColumn) + " LIKE ?", []any{numeric + "%"}
}

func contactSearchCompactExpression(field string) string {
	return "REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(LOWER(" + field + "), ' ', ''), '_', ''), '-', ''), '.', ''), '@', '')"
}

func contactSearchIDExpression(column string) string {
	switch store.DB.Dialector.Name() {
	case "mysql":
		return "CAST(" + column + " AS CHAR)"
	default:
		return "CAST(" + column + " AS TEXT)"
	}
}

func rankContactSearchItems(items []ContactSearchItem, keyword contactSearchKeyword) []ContactSearchItem {
	if len(items) == 0 {
		return items
	}

	ranked := make([]rankedContactSearchItem, 0, len(items))
	for _, item := range items {
		ranked = append(ranked, rankedContactSearchItem{
			item:  item,
			score: contactSearchScore(item, keyword),
		})
	}

	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return contactSearchItemLess(ranked[i].item, ranked[j].item)
	})

	result := make([]ContactSearchItem, 0, len(ranked))
	for _, entry := range ranked {
		result = append(result, entry.item)
	}
	return result
}

func sortContactSearchItems(items []ContactSearchItem) []ContactSearchItem {
	sort.SliceStable(items, func(i, j int) bool {
		return contactSearchItemLess(items[i], items[j])
	})
	return items
}

func contactSearchItemLess(left, right ContactSearchItem) bool {
	if left.CreatedAt.Equal(right.CreatedAt) {
		if left.PeerType == right.PeerType {
			return left.PeerID < right.PeerID
		}
		return left.PeerType < right.PeerType
	}
	return left.CreatedAt.After(right.CreatedAt)
}

func contactSearchScore(item ContactSearchItem, keyword contactSearchKeyword) int {
	score := scoreContactID(item.PeerID, keyword)
	switch item.PeerType {
	case contactPeerTypeUser:
		score = maxInt(score, scoreContactField(item.RemarkName, keyword, 60))
		score = maxInt(score, scoreContactField(item.Nickname, keyword, 40))
		score = maxInt(score, scoreContactField(item.Username, keyword, 20))
	case contactPeerTypeAgent:
		score = maxInt(score, scoreContactField(item.DisplayName, keyword, 50))
	default:
		score = maxInt(score, scoreContactField(item.DisplayName, keyword, 10))
	}
	return score
}

func scoreContactField(field string, keyword contactSearchKeyword, fieldBonus int) int {
	normalized := strings.ToLower(strings.TrimSpace(field))
	if normalized == "" {
		return 0
	}

	score := 0
	switch {
	case normalized == keyword.lowered:
		score = maxInt(score, 3000+fieldBonus)
	case strings.HasPrefix(normalized, keyword.lowered):
		score = maxInt(score, 2400+fieldBonus)
	case strings.Contains(normalized, keyword.lowered):
		score = maxInt(score, 1800+fieldBonus)
	}

	compact := compactSearchText(field)
	if keyword.compact != "" && compact != "" {
		switch {
		case compact == keyword.compact:
			score = maxInt(score, 2900+fieldBonus)
		case strings.HasPrefix(compact, keyword.compact):
			score = maxInt(score, 2300+fieldBonus)
		case strings.Contains(compact, keyword.compact):
			score = maxInt(score, 1700+fieldBonus)
		}
	}

	if len(keyword.tokens) > 1 {
		if containsAllTokens(normalized, keyword.tokens) {
			score = maxInt(score, 2100+fieldBonus)
		}
		if compact != "" && containsAllTokens(compact, keyword.tokens) {
			score = maxInt(score, 2200+fieldBonus)
		}
	}

	return score
}

func scoreContactID(id int64, keyword contactSearchKeyword) int {
	if keyword.numeric == "" {
		return 0
	}
	idText := strconv.FormatInt(id, 10)
	switch {
	case idText == keyword.numeric:
		return 3200
	case strings.HasPrefix(idText, keyword.numeric):
		return 2600
	default:
		return 0
	}
}

func containsAllTokens(text string, tokens []string) bool {
	if text == "" || len(tokens) == 0 {
		return false
	}
	for _, token := range tokens {
		if !strings.Contains(text, token) {
			return false
		}
	}
	return true
}

func maxInt(left, right int) int {
	if right > left {
		return right
	}
	return left
}
