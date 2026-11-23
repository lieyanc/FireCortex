package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	auditapp "github.com/lieyanc/FireCortex/backend/internal/application/audit"
	userapp "github.com/lieyanc/FireCortex/backend/internal/application/user"
	domainconversation "github.com/lieyanc/FireCortex/backend/internal/domain/conversation"
	domainuser "github.com/lieyanc/FireCortex/backend/internal/domain/user"
	"github.com/lieyanc/FireCortex/backend/internal/pkg/textutil"
	"github.com/lieyanc/FireCortex/backend/internal/repository"
)

const (
	openWebUIConversationImportSource    = "openwebui"
	openWebUIConversationDedupeField     = "session_key"
	openWebUIConversationDedupeRule      = "Imported conversations are matched by source chat ID per user and skipped without modification."
	openWebUIConversationImportMaxDays   = 36500
	openWebUIConversationMaxTags         = 20
	openWebUIConversationFallbackTitle   = "未命名对话"
	openWebUIConversationAuditAction     = "admin_import_openwebui_conversations"
	openWebUIConversationAuditResource   = "conversation"
	openWebUIConversationTimestampMillis = 1e11
	openWebUIConversationTimestampMicros = 1e14
	openWebUIConversationTimestampNanos  = 1e17
)

var (
	reasoningDetailsPattern = regexp.MustCompile(`(?is)<details\b([^>]*)>([\s\S]*?)</details>`)
	detailsTypePattern      = regexp.MustCompile(`(?i)\btype\s*=\s*(?:"reasoning"|'reasoning'|reasoning\b)`)
	detailsDurationPattern  = regexp.MustCompile(`(?i)\bduration\s*=\s*["']?(\d+(?:\.\d+)?)["']?`)
	summaryTagPattern       = regexp.MustCompile(`(?is)<summary\b[^>]*>[\s\S]*?</summary>`)
	thinkTagPattern         = regexp.MustCompile(`(?is)<think\b[^>]*>([\s\S]*?)</think>`)
)

type openWebUIChatLoader interface {
	LoadOpenWebUIChats(ctx context.Context, dsn string, cutoff *time.Time) (*repository.OpenWebUIChatLoadResult, error)
}

type openWebUIConversationUserLookup interface {
	ListUsersByLowerEmails(ctx context.Context, emails []string) (map[string]domainuser.User, error)
}

type openWebUIChatPayload struct {
	ID        string                    `json:"id"`
	Title     string                    `json:"title"`
	Models    []string                  `json:"models"`
	Tags      []string                  `json:"tags"`
	Timestamp any                       `json:"timestamp"`
	History   *openWebUIHistoryPayload  `json:"history"`
	Messages  []openWebUIMessagePayload `json:"messages"`
}

type openWebUIHistoryPayload struct {
	CurrentID string                             `json:"currentId"`
	Messages  map[string]openWebUIMessagePayload `json:"messages"`
}

type openWebUIMessagePayload struct {
	ID               string          `json:"id"`
	ParentID         string          `json:"parentId"`
	ChildrenIDs      []string        `json:"childrenIds"`
	Role             string          `json:"role"`
	Content          json.RawMessage `json:"content"`
	Model            string          `json:"model"`
	ModelName        string          `json:"modelName"`
	Timestamp        any             `json:"timestamp"`
	ReasoningContent string          `json:"reasoning_content"`
	Reasoning        string          `json:"reasoning"`
	Details          json.RawMessage `json:"details"`
}

type openWebUIDetailItem struct {
	Type     string  `json:"type"`
	Content  string  `json:"content"`
	Text     string  `json:"text"`
	Duration float64 `json:"duration"`
}

type openWebUIMetaPayload struct {
	Tags []string `json:"tags"`
}

type openWebUIMessagePart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type openWebUIConversationCandidate struct {
	sessionKey string
	record     repository.ConversationImportRecord
}

// ImportOpenWebUIConversations 从 OpenWebUI 数据库导入对话历史记录，支持可选按近 X 天过滤与预览。
func (s *Service) ImportOpenWebUIConversations(
	ctx context.Context,
	requestID string,
	actorUserID uint,
	input OpenWebUIConversationImportInput,
	ip string,
	userAgent string,
) (*OpenWebUIConversationImportResult, error) {
	if _, err := s.getActorUser(ctx, actorUserID); err != nil {
		return nil, err
	}
	if input.RecentDays != nil && (*input.RecentDays <= 0 || *input.RecentDays > openWebUIConversationImportMaxDays) {
		return nil, ErrInvalidImportRecentDays
	}
	if s.openWebUIChatLoader == nil || s.conversationImportRepo == nil {
		return nil, ErrOpenWebUIImportFailed
	}

	var userLookup openWebUIConversationUserLookup
	if !s.localMode {
		var ok bool
		userLookup, ok = s.userService.(openWebUIConversationUserLookup)
		if !ok {
			return nil, ErrOpenWebUIImportFailed
		}
	}

	var recentDaysCopy *int
	var cutoff *time.Time
	now := time.Now().UTC()
	if input.RecentDays != nil {
		days := *input.RecentDays
		cutoffVal := now.Add(-time.Duration(days) * 24 * time.Hour)
		recentDaysCopy = &days
		cutoff = &cutoffVal
	}

	loadResult, err := s.openWebUIChatLoader.LoadOpenWebUIChats(ctx, input.DSN, cutoff)
	if err != nil {
		if errors.Is(err, repository.ErrInvalidInput) {
			return nil, ErrInvalidImportDSN
		}
		return nil, ErrOpenWebUIImportFailed
	}
	if loadResult == nil {
		loadResult = &repository.OpenWebUIChatLoadResult{}
	}

	scanned := max(loadResult.TotalCount, len(loadResult.Rows))
	skippedOutside := max(0, loadResult.TotalCount-len(loadResult.Rows))
	result := &OpenWebUIConversationImportResult{
		Source:                  openWebUIConversationImportSource,
		DedupeField:             openWebUIConversationDedupeField,
		DedupeRule:              openWebUIConversationDedupeRule,
		RecentDays:              recentDaysCopy,
		CutoffAt:                cutoff,
		Scanned:                 scanned,
		SkippedOutsideTimeRange: skippedOutside,
	}

	if len(loadResult.Rows) == 0 {
		if !input.DryRun {
			s.writeOpenWebUIConversationImportAudit(ctx, requestID, actorUserID, ip, userAgent, result)
		}
		return result, nil
	}

	var existingUsersByEmail map[string]domainuser.User
	if !s.localMode {
		emailKeys := collectNormalizedOpenWebUIEmails(loadResult.Rows)
		existingUsersByEmail, err = userLookup.ListUsersByLowerEmails(ctx, emailKeys)
		if err != nil {
			return nil, err
		}
	}

	candidates := make([]openWebUIConversationCandidate, 0, len(loadResult.Rows))
	sessionKeys := make([]string, 0, len(loadResult.Rows))
	seenSessionKeys := make(map[string]struct{}, len(loadResult.Rows))

	for _, row := range loadResult.Rows {
		payload, ok := parseOpenWebUIChatPayload(row.ChatJSON)
		if !ok {
			result.SkippedInvalidRow++
			continue
		}
		sourceChatID := textutil.FirstNonEmpty(row.SourceChatID, payload.ID)
		if sourceChatID == "" {
			result.SkippedInvalidRow++
			continue
		}

		rowCreatedAt := parseOpenWebUITimestamp(row.CreatedAtRaw)
		rowUpdatedAt := parseOpenWebUITimestamp(row.UpdatedAtRaw)
		payloadTimestamp := parseOpenWebUITimestamp(payload.Timestamp)
		msgMinTime, msgMaxTime := extractOpenWebUIMessageTimeBounds(payload)

		if cutoff != nil {
			activityAt := firstNonZeroTime(rowUpdatedAt, rowCreatedAt, msgMaxTime, payloadTimestamp)
			if activityAt.IsZero() || activityAt.Before(*cutoff) {
				result.SkippedOutsideTimeRange++
				continue
			}
		}

		targetUserID, matched := resolveOpenWebUIConversationTargetUser(s.localMode, actorUserID, row.UserEmail, existingUsersByEmail)
		if !matched {
			result.SkippedUnmatchedUser++
			continue
		}

		sessionKey := resolveOpenWebUIConversationSessionKey(targetUserID, sourceChatID)
		if _, dup := seenSessionKeys[sessionKey]; dup {
			result.SkippedExistingConversation++
			continue
		}
		seenSessionKeys[sessionKey] = struct{}{}

		convCreatedAt := firstNonZeroTime(rowCreatedAt, payloadTimestamp, msgMinTime, rowUpdatedAt, now)
		convUpdatedAt := firstNonZeroTime(rowUpdatedAt, msgMaxTime, convCreatedAt)
		if convUpdatedAt.Before(convCreatedAt) {
			convUpdatedAt = convCreatedAt
		}

		messages, lastActiveModel := buildOpenWebUIConversationMessages(targetUserID, sourceChatID, payload, convCreatedAt)
		if len(messages) == 0 {
			result.SkippedEmptyConversation++
			continue
		}

		title := resolveOpenWebUIConversationTitle(row.Title, payload.Title, messages)
		modelName := textutil.TruncateTrimmed(resolveOpenWebUIConversationModel(lastActiveModel, payload.Models), 128)
		labelsJSON, labelsManual := resolveOpenWebUIConversationLabels(row.MetaJSON, payload.Tags)

		status := "active"
		if row.Archived {
			status = "archived"
		}
		var starredAt *time.Time
		if row.Pinned {
			starredAtVal := convUpdatedAt
			starredAt = &starredAtVal
		}

		candidates = append(candidates, openWebUIConversationCandidate{
			sessionKey: sessionKey,
			record: repository.ConversationImportRecord{
				Conversation: domainconversation.Conversation{
					UserID:                targetUserID,
					PublicID:              resolveOpenWebUIConversationPublicID(targetUserID, sourceChatID),
					Title:                 title,
					LabelsJSON:            labelsJSON,
					LabelsManuallyManaged: labelsManual,
					Model:                 modelName,
					ModelMode:             "manual",
					Provider:              inferOpenWebUIProvider(modelName),
					SessionKey:            sessionKey,
					IsStarred:             row.Pinned,
					StarredAt:             starredAt,
					MessageCount:          len(messages),
					Status:                status,
					ContextPolicy:         "{}",
					CreatedAt:             convCreatedAt,
					UpdatedAt:             convUpdatedAt,
				},
				Messages: messages,
			},
		})
		sessionKeys = append(sessionKeys, sessionKey)
	}

	if len(candidates) == 0 {
		if !input.DryRun {
			s.writeOpenWebUIConversationImportAudit(ctx, requestID, actorUserID, ip, userAgent, result)
		}
		return result, nil
	}

	existingSessionKeys, err := s.conversationImportRepo.ListExistingConversationSessionKeys(ctx, sessionKeys)
	if err != nil {
		return nil, err
	}

	pending := make([]openWebUIConversationCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if _, exists := existingSessionKeys[candidate.sessionKey]; exists {
			result.SkippedExistingConversation++
			continue
		}
		pending = append(pending, candidate)
	}

	if input.DryRun {
		result.Imported = len(pending)
		for _, candidate := range pending {
			result.ImportedMessages += len(candidate.record.Messages)
		}
		return result, nil
	}

	for _, candidate := range pending {
		if err = s.conversationImportRepo.ImportConversation(ctx, candidate.record); err != nil {
			if errors.Is(err, repository.ErrDuplicate) {
				result.SkippedExistingConversation++
				continue
			}
			return nil, err
		}
		result.Imported++
		result.ImportedMessages += len(candidate.record.Messages)
	}

	s.writeOpenWebUIConversationImportAudit(ctx, requestID, actorUserID, ip, userAgent, result)
	return result, nil
}

func collectNormalizedOpenWebUIEmails(rows []repository.OpenWebUIChatRow) []string {
	emails := make([]string, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		normalized, err := userapp.NormalizeEmail(row.UserEmail)
		if err != nil {
			continue
		}
		lower := strings.ToLower(strings.TrimSpace(normalized))
		if lower == "" {
			continue
		}
		if _, exists := seen[lower]; exists {
			continue
		}
		seen[lower] = struct{}{}
		emails = append(emails, lower)
	}
	return emails
}

func resolveOpenWebUIConversationTargetUser(
	localMode bool,
	actorUserID uint,
	userEmail string,
	existingByEmail map[string]domainuser.User,
) (uint, bool) {
	if localMode {
		return actorUserID, true
	}
	normalized, err := userapp.NormalizeEmail(userEmail)
	if err != nil {
		return 0, false
	}
	lower := strings.ToLower(strings.TrimSpace(normalized))
	if lower == "" {
		return 0, false
	}
	matched, ok := existingByEmail[lower]
	if !ok || matched.ID == 0 {
		return 0, false
	}
	return matched.ID, true
}

func parseOpenWebUIChatPayload(raw string) (openWebUIChatPayload, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return openWebUIChatPayload{}, false
	}
	var payload openWebUIChatPayload
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return openWebUIChatPayload{}, false
	}
	return payload, true
}

func resolveOpenWebUIConversationSessionKey(targetUserID uint, sourceChatID string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s", targetUserID, strings.TrimSpace(sourceChatID))))
	return "openwebui:" + hex.EncodeToString(sum[:])
}

func resolveOpenWebUIConversationPublicID(targetUserID uint, sourceChatID string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("conv\x00%d\x00%s", targetUserID, strings.TrimSpace(sourceChatID))))
	return "owc" + hex.EncodeToString(sum[:])[:29]
}

func resolveOpenWebUIMessagePublicID(targetUserID uint, sourceChatID string, nodeID string, index int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("msg\x00%d\x00%s\x00%s\x00%d", targetUserID, strings.TrimSpace(sourceChatID), strings.TrimSpace(nodeID), index)))
	return "owm" + hex.EncodeToString(sum[:])[:29]
}

func parseOpenWebUITimestamp(raw any) time.Time {
	switch v := raw.(type) {
	case nil:
		return time.Time{}
	case time.Time:
		if v.IsZero() {
			return time.Time{}
		}
		return v.UTC()
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" || trimmed == "0" || strings.EqualFold(trimmed, "null") {
			return time.Time{}
		}
		if num, err := strconv.ParseFloat(trimmed, 64); err == nil {
			return epochNumberToTime(num)
		}
		for _, layout := range []string{
			time.RFC3339Nano,
			time.RFC3339,
			"2006-01-02 15:04:05.999999999Z07:00",
			"2006-01-02 15:04:05.999999999-07:00",
			"2006-01-02 15:04:05.999999999-07",
			"2006-01-02 15:04:05.999999999",
			"2006-01-02T15:04:05.999999999",
			"2006-01-02 15:04:05",
			"2006-01-02",
		} {
			if parsed, err := time.Parse(layout, trimmed); err == nil {
				return parsed.UTC()
			}
		}
		return time.Time{}
	case json.Number:
		if num, err := v.Float64(); err == nil {
			return epochNumberToTime(num)
		}
		return time.Time{}
	case float64:
		return epochNumberToTime(v)
	case float32:
		return epochNumberToTime(float64(v))
	case int64:
		return epochNumberToTime(float64(v))
	case int:
		return epochNumberToTime(float64(v))
	default:
		return time.Time{}
	}
}

func epochNumberToTime(num float64) time.Time {
	if num <= 0 || math.IsNaN(num) || math.IsInf(num, 0) {
		return time.Time{}
	}
	switch {
	case num >= openWebUIConversationTimestampNanos:
		return time.Unix(0, int64(num)).UTC()
	case num >= openWebUIConversationTimestampMicros:
		return time.UnixMicro(int64(num)).UTC()
	case num >= openWebUIConversationTimestampMillis:
		return time.UnixMilli(int64(num)).UTC()
	default:
		sec := int64(num)
		nsec := int64(math.Round((num - float64(sec)) * 1e9))
		return time.Unix(sec, nsec).UTC()
	}
}

func firstNonZeroTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value.UTC()
		}
	}
	return time.Time{}
}

func extractOpenWebUIMessageTimeBounds(payload openWebUIChatPayload) (time.Time, time.Time) {
	var minTime, maxTime time.Time
	update := func(raw any) {
		ts := parseOpenWebUITimestamp(raw)
		if ts.IsZero() {
			return
		}
		if minTime.IsZero() || ts.Before(minTime) {
			minTime = ts
		}
		if maxTime.IsZero() || ts.After(maxTime) {
			maxTime = ts
		}
	}
	if payload.History != nil {
		for _, item := range payload.History.Messages {
			update(item.Timestamp)
		}
	}
	for _, item := range payload.Messages {
		update(item.Timestamp)
	}
	return minTime, maxTime
}

type openWebUITreeNode struct {
	id          string
	parentID    string
	childrenIDs []string
	timestamp   time.Time
	payload     openWebUIMessagePayload
}

func buildOpenWebUIConversationMessages(
	targetUserID uint,
	sourceChatID string,
	payload openWebUIChatPayload,
	fallbackTime time.Time,
) ([]repository.ImportedConversationMessage, string) {
	if payload.History != nil && len(payload.History.Messages) > 0 {
		return buildOpenWebUITreeMessages(targetUserID, sourceChatID, payload.History, fallbackTime)
	}
	return buildOpenWebUIFlatMessages(targetUserID, sourceChatID, payload.Messages, fallbackTime)
}

func buildOpenWebUITreeMessages(
	targetUserID uint,
	sourceChatID string,
	history *openWebUIHistoryPayload,
	fallbackTime time.Time,
) ([]repository.ImportedConversationMessage, string) {
	nodes := make(map[string]*openWebUITreeNode, len(history.Messages))
	allIDs := make([]string, 0, len(history.Messages))
	mapKeys := make([]string, 0, len(history.Messages))
	for key := range history.Messages {
		mapKeys = append(mapKeys, key)
	}
	slices.Sort(mapKeys)
	for _, key := range mapKeys {
		msg := history.Messages[key]
		id := textutil.FirstNonEmpty(msg.ID, key)
		if id == "" {
			continue
		}
		if _, exists := nodes[id]; exists {
			continue
		}
		nodes[id] = &openWebUITreeNode{
			id:          id,
			parentID:    strings.TrimSpace(msg.ParentID),
			childrenIDs: msg.ChildrenIDs,
			timestamp:   parseOpenWebUITimestamp(msg.Timestamp),
			payload:     msg,
		}
		allIDs = append(allIDs, id)
	}
	if len(nodes) == 0 {
		return nil, ""
	}

	// 建立父子关系并保留 childrenIds 顺序；对仅存在于 childrenIds 但缺失 parentId 的节点补齐父指针。
	childrenByParent := make(map[string][]string, len(nodes))
	childSeen := make(map[string]map[string]struct{}, len(nodes))
	addChild := func(parentID string, childID string) {
		if parentID == "" || childID == "" || parentID == childID {
			return
		}
		if _, ok := nodes[parentID]; !ok {
			return
		}
		if _, ok := nodes[childID]; !ok {
			return
		}
		seen := childSeen[parentID]
		if seen == nil {
			seen = make(map[string]struct{})
			childSeen[parentID] = seen
		}
		if _, exists := seen[childID]; exists {
			return
		}
		seen[childID] = struct{}{}
		childrenByParent[parentID] = append(childrenByParent[parentID], childID)
	}

	for _, id := range allIDs {
		node := nodes[id]
		for _, rawChildID := range node.childrenIDs {
			childID := strings.TrimSpace(rawChildID)
			if child, ok := nodes[childID]; ok && child.parentID == "" {
				child.parentID = id
			}
			addChild(id, childID)
		}
	}

	extraChildrenByParent := make(map[string][]string)
	roots := make([]string, 0)
	for _, id := range allIDs {
		node := nodes[id]
		if node.parentID == "" || node.parentID == id {
			node.parentID = ""
			roots = append(roots, id)
			continue
		}
		if _, hasParent := nodes[node.parentID]; !hasParent {
			node.parentID = ""
			roots = append(roots, id)
			continue
		}
		if seen := childSeen[node.parentID]; seen == nil {
			extraChildrenByParent[node.parentID] = append(extraChildrenByParent[node.parentID], id)
		} else if _, exists := seen[id]; !exists {
			extraChildrenByParent[node.parentID] = append(extraChildrenByParent[node.parentID], id)
		}
	}

	sortByTimeAndID := func(ids []string) {
		slices.SortFunc(ids, func(a string, b string) int {
			ta, tb := nodes[a].timestamp, nodes[b].timestamp
			if !ta.Equal(tb) {
				if ta.IsZero() {
					return 1
				}
				if tb.IsZero() {
					return -1
				}
				return ta.Compare(tb)
			}
			return strings.Compare(a, b)
		})
	}

	for parentID, extras := range extraChildrenByParent {
		sortByTimeAndID(extras)
		for _, childID := range extras {
			addChild(parentID, childID)
		}
	}
	sortByTimeAndID(roots)

	activeChain := buildOpenWebUIActiveChain(nodes, history.CurrentID)
	orderSiblings := func(ids []string) []string {
		if len(ids) <= 1 {
			return ids
		}
		nonActive := make([]string, 0, len(ids))
		active := make([]string, 0, 1)
		for _, id := range ids {
			if activeChain[id] {
				active = append(active, id)
			} else {
				nonActive = append(nonActive, id)
			}
		}
		return append(nonActive, active...)
	}

	roots = orderSiblings(roots)
	visited := make(map[string]struct{}, len(nodes))
	results := make([]repository.ImportedConversationMessage, 0, len(nodes))
	lastActiveModel := ""

	var visit func(nodeID string, parentIndex int)
	visit = func(nodeID string, parentIndex int) {
		if _, seen := visited[nodeID]; seen {
			return
		}
		visited[nodeID] = struct{}{}
		node := nodes[nodeID]

		nextParentIndex := parentIndex
		if built, modelName, ok := buildImportedMessage(targetUserID, sourceChatID, node.id, node.payload, len(results), parentIndex, fallbackTime); ok {
			nextParentIndex = len(results)
			results = append(results, built)
			if modelName != "" && (activeChain[node.id] || lastActiveModel == "") {
				lastActiveModel = modelName
			}
		}

		children := orderSiblings(childrenByParent[nodeID])
		for _, childID := range children {
			visit(childID, nextParentIndex)
		}
	}

	for _, rootID := range roots {
		visit(rootID, -1)
	}
	// 处理孤立环（无根节点可达的异常数据），按时间与 ID 顺序断开环后导入。
	if len(visited) < len(allIDs) {
		remaining := make([]string, 0, len(allIDs)-len(visited))
		for _, id := range allIDs {
			if _, seen := visited[id]; !seen {
				remaining = append(remaining, id)
			}
		}
		sortByTimeAndID(remaining)
		for _, id := range remaining {
			visit(id, -1)
		}
	}

	return results, lastActiveModel
}

func buildOpenWebUIActiveChain(nodes map[string]*openWebUITreeNode, currentID string) map[string]bool {
	chain := make(map[string]bool)
	cursor := strings.TrimSpace(currentID)
	for cursor != "" {
		if chain[cursor] {
			break
		}
		node, ok := nodes[cursor]
		if !ok {
			break
		}
		chain[cursor] = true
		cursor = node.parentID
	}
	return chain
}

func buildOpenWebUIFlatMessages(
	targetUserID uint,
	sourceChatID string,
	items []openWebUIMessagePayload,
	fallbackTime time.Time,
) ([]repository.ImportedConversationMessage, string) {
	results := make([]repository.ImportedConversationMessage, 0, len(items))
	lastModel := ""
	for index, item := range items {
		nodeID := textutil.FirstNonEmpty(item.ID, fmt.Sprintf("flat_%d", index))
		parentIndex := len(results) - 1
		built, modelName, ok := buildImportedMessage(targetUserID, sourceChatID, nodeID, item, len(results), parentIndex, fallbackTime)
		if !ok {
			continue
		}
		results = append(results, built)
		if modelName != "" {
			lastModel = modelName
		}
	}
	return results, lastModel
}

func buildImportedMessage(
	targetUserID uint,
	sourceChatID string,
	nodeID string,
	item openWebUIMessagePayload,
	index int,
	parentIndex int,
	fallbackTime time.Time,
) (repository.ImportedConversationMessage, string, bool) {
	role := normalizeOpenWebUIMessageRole(item.Role)
	if role == "" {
		return repository.ImportedConversationMessage{}, "", false
	}

	rawContent := extractOpenWebUIMessageText(item.Content)
	content := rawContent
	var reasoning string
	var reasoningDurationSec int
	if role == "assistant" {
		content, reasoning, reasoningDurationSec = extractOpenWebUIReasoning(rawContent, item)
	} else {
		content = strings.TrimSpace(content)
	}

	content = textutil.SanitizeForTextColumn(content)
	reasoning = textutil.SanitizeForTextColumn(reasoning)
	if strings.TrimSpace(content) == "" && strings.TrimSpace(reasoning) == "" {
		return repository.ImportedConversationMessage{}, "", false
	}

	msgTime := firstNonZeroTime(parseOpenWebUITimestamp(item.Timestamp), fallbackTime)
	modelName := textutil.FirstNonEmpty(item.Model, item.ModelName)

	return repository.ImportedConversationMessage{
		ParentIndex:          parentIndex,
		ReasoningDurationSec: reasoningDurationSec,
		Message: domainconversation.Message{
			UserID:           targetUserID,
			PublicID:         resolveOpenWebUIMessagePublicID(targetUserID, sourceChatID, nodeID, index),
			Role:             role,
			ContentType:      "text",
			Content:          content,
			ReasoningContent: reasoning,
			BranchReason:     "default",
			BilledCurrency:   "USD",
			Status:           "success",
			CreatedAt:        msgTime,
			UpdatedAt:        msgTime,
		},
	}, modelName, true
}

func normalizeOpenWebUIMessageRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "user":
		return "user"
	case "assistant":
		return "assistant"
	case "system":
		return "system"
	default:
		return ""
	}
}

func extractOpenWebUIMessageText(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var parts []openWebUIMessagePart
	if err := json.Unmarshal(raw, &parts); err == nil {
		lines := make([]string, 0, len(parts))
		for _, part := range parts {
			partType := strings.ToLower(strings.TrimSpace(part.Type))
			if partType != "" && partType != "text" {
				continue
			}
			if part.Text != "" {
				lines = append(lines, part.Text)
			}
		}
		return strings.Join(lines, "\n")
	}
	return ""
}

func extractOpenWebUIReasoning(rawContent string, item openWebUIMessagePayload) (string, string, int) {
	reasoningParts := make([]string, 0, 4)
	seenParts := make(map[string]struct{})
	totalDuration := 0

	appendReasoning := func(part string) {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			return
		}
		if _, exists := seenParts[trimmed]; exists {
			return
		}
		seenParts[trimmed] = struct{}{}
		reasoningParts = append(reasoningParts, trimmed)
	}

	cleaned := rawContent
	// 1. 提取 OpenWebUI <details type="reasoning" ...>...</details> 块。
	matches := reasoningDetailsPattern.FindAllStringSubmatchIndex(cleaned, -1)
	if len(matches) > 0 {
		var rebuilt strings.Builder
		cursor := 0
		for _, m := range matches {
			fullStart, fullEnd := m[0], m[1]
			attrs := cleaned[m[2]:m[3]]
			if !detailsTypePattern.MatchString(attrs) {
				continue
			}
			rebuilt.WriteString(cleaned[cursor:fullStart])
			cursor = fullEnd

			if dur := parseDetailsDurationSeconds(attrs); dur > 0 {
				totalDuration += dur
			}
			body := cleaned[m[4]:m[5]]
			body = summaryTagPattern.ReplaceAllString(body, "")
			appendReasoning(unquoteBlockquoteLines(body))
		}
		rebuilt.WriteString(cleaned[cursor:])
		cleaned = rebuilt.String()
	}

	// 2. 提取 <think>...</think> 标签。
	thinkMatches := thinkTagPattern.FindAllStringSubmatchIndex(cleaned, -1)
	if len(thinkMatches) > 0 {
		var rebuilt strings.Builder
		cursor := 0
		for _, m := range thinkMatches {
			rebuilt.WriteString(cleaned[cursor:m[0]])
			cursor = m[1]
			appendReasoning(cleaned[m[2]:m[3]])
		}
		rebuilt.WriteString(cleaned[cursor:])
		cleaned = rebuilt.String()
	}

	// 3. 提取结构化字段 reasoning_content / reasoning / details[]。
	appendReasoning(item.ReasoningContent)
	appendReasoning(item.Reasoning)
	if len(item.Details) > 0 {
		var details []openWebUIDetailItem
		if json.Unmarshal(item.Details, &details) == nil {
			for _, detail := range details {
				if !strings.EqualFold(strings.TrimSpace(detail.Type), "reasoning") {
					continue
				}
				appendReasoning(textutil.FirstNonEmpty(detail.Content, detail.Text))
				if detail.Duration > 0 {
					totalDuration += int(math.Round(detail.Duration))
				}
			}
		}
	}

	return strings.TrimSpace(cleaned), strings.Join(reasoningParts, "\n\n"), totalDuration
}

func parseDetailsDurationSeconds(attrs string) int {
	match := detailsDurationPattern.FindStringSubmatch(attrs)
	if len(match) < 2 {
		return 0
	}
	parsed, err := strconv.ParseFloat(match[1], 64)
	if err != nil || parsed <= 0 || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0
	}
	return int(math.Round(parsed))
}

func unquoteBlockquoteLines(body string) string {
	normalized := strings.ReplaceAll(body, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	for index, line := range lines {
		switch {
		case strings.HasPrefix(line, "> "):
			lines[index] = line[2:]
		case strings.HasPrefix(line, ">"):
			lines[index] = line[1:]
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func resolveOpenWebUIConversationTitle(rowTitle string, payloadTitle string, messages []repository.ImportedConversationMessage) string {
	title := textutil.SanitizeForTextColumn(textutil.TruncateTrimmed(textutil.FirstNonEmpty(rowTitle, payloadTitle), 255))
	if title != "" {
		return title
	}
	for _, item := range messages {
		if item.Message.Role == "user" && strings.TrimSpace(item.Message.Content) != "" {
			snippet := textutil.CompactSnippet(item.Message.Content, 40)
			if snippet != "" {
				return textutil.SanitizeForTextColumn(textutil.TruncateTrimmed(snippet, 255))
			}
		}
	}
	return openWebUIConversationFallbackTitle
}

func resolveOpenWebUIConversationModel(lastActiveModel string, payloadModels []string) string {
	if trimmed := strings.TrimSpace(lastActiveModel); trimmed != "" {
		return trimmed
	}
	for _, item := range payloadModels {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func resolveOpenWebUIConversationLabels(metaJSON string, payloadTags []string) (string, bool) {
	var meta openWebUIMetaPayload
	if trimmed := strings.TrimSpace(metaJSON); trimmed != "" {
		_ = json.Unmarshal([]byte(trimmed), &meta)
	}

	tags := make([]string, 0, len(meta.Tags)+len(payloadTags))
	seen := make(map[string]struct{}, len(meta.Tags)+len(payloadTags))
	appendTag := func(raw string) {
		if len(tags) >= openWebUIConversationMaxTags {
			return
		}
		cleaned := textutil.SanitizeForTextColumn(textutil.TruncateTrimmed(raw, 64))
		if cleaned == "" {
			return
		}
		key := strings.ToLower(cleaned)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		tags = append(tags, cleaned)
	}

	for _, tag := range meta.Tags {
		appendTag(tag)
	}
	for _, tag := range payloadTags {
		appendTag(tag)
	}

	if len(tags) == 0 {
		return "[]", false
	}
	encoded, err := json.Marshal(tags)
	if err != nil {
		return "[]", false
	}
	return string(encoded), true
}

func inferOpenWebUIProvider(modelName string) string {
	code := strings.ToLower(strings.TrimSpace(modelName))
	switch {
	case code == "":
		return ""
	case strings.HasPrefix(code, "gpt-"):
		return "openai"
	case strings.HasPrefix(code, "claude-"):
		return "anthropic"
	default:
		return "internal"
	}
}

func (s *Service) writeOpenWebUIConversationImportAudit(
	ctx context.Context,
	requestID string,
	actorUserID uint,
	ip string,
	userAgent string,
	result *OpenWebUIConversationImportResult,
) {
	if result == nil {
		return
	}
	detail := map[string]any{
		"source":                        result.Source,
		"dedupe_field":                  result.DedupeField,
		"scanned":                       result.Scanned,
		"imported":                      result.Imported,
		"imported_messages":             result.ImportedMessages,
		"skipped_outside_time_range":    result.SkippedOutsideTimeRange,
		"skipped_existing_conversation": result.SkippedExistingConversation,
		"skipped_unmatched_user":        result.SkippedUnmatchedUser,
		"skipped_empty_conversation":    result.SkippedEmptyConversation,
		"skipped_invalid_row":           result.SkippedInvalidRow,
	}
	if result.RecentDays != nil {
		detail["recent_days"] = *result.RecentDays
	}
	s.auditService.Write(ctx, auditapp.WriteInput{
		RequestID:   requestID,
		ActorUserID: actorUserID,
		Action:      openWebUIConversationAuditAction,
		Resource:    openWebUIConversationAuditResource,
		ResourceID:  openWebUIConversationImportSource,
		IP:          ip,
		UserAgent:   userAgent,
		Detail:      detail,
	})
}
