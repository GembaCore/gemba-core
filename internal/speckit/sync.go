package speckit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/GembaCore/gemba-core/core"
)

const (
	LabelSource    = "source:spec-kit"
	LabelMilestone = "speckit:milestone"
	LabelEpic      = "speckit:epic"
	LabelStory     = "speckit:story"
	LabelTask      = "speckit:task"
)

type desiredItem struct {
	Key      string
	SourceID string
	Item     core.WorkItem
}

func PlanFeature(ctx context.Context, wp core.WorkPlane, feature Feature) (SyncPlan, error) {
	if wp == nil {
		return SyncPlan{}, fmt.Errorf("spec kit plan: WorkPlane is nil")
	}
	if strings.TrimSpace(feature.ID) == "" {
		return SyncPlan{}, fmt.Errorf("spec kit plan: feature id is required")
	}
	existing, err := wp.ListWorkItems(ctx, core.WorkItemFilter{
		Labels: []string{LabelSource, featureLabel(feature.ID)},
		Limit:  1000,
	})
	if err != nil {
		return SyncPlan{}, err
	}
	index := indexExisting(existing)
	desired := desiredItems(feature, index)
	desiredKeys := map[string]bool{}
	plan := SyncPlan{FeatureID: feature.ID}
	for _, d := range desired {
		desiredKeys[d.Key] = true
		if ex, ok := index[d.Key]; ok {
			plan.addChange(ChangeUpdate, d.Key, d.SourceID, ex.ID, d.Item.Kind, d.Item.Title)
		} else {
			plan.addChange(ChangeCreate, d.Key, d.SourceID, "", d.Item.Kind, d.Item.Title)
		}
	}
	for key, ex := range index {
		if !desiredKeys[key] {
			plan.addChange(ChangeDelete, key, sourceIDFromKey(key), ex.ID, ex.Kind, ex.Title)
		}
	}
	plan.JSONL = ManifestJSONL(desired)
	if err := ValidateManifestJSONL(plan.JSONL); err != nil {
		plan.Warnings = append(plan.Warnings, "JSONL manifest is not bd-importable: "+err.Error())
	}
	plan.finalize()
	return plan, nil
}

func DraftFeature(ctx context.Context, wp core.WorkPlane, feature Feature) (SyncDraft, error) {
	if wp == nil {
		return SyncDraft{}, fmt.Errorf("spec kit draft: WorkPlane is nil")
	}
	if strings.TrimSpace(feature.ID) == "" {
		return SyncDraft{}, fmt.Errorf("spec kit draft: feature id is required")
	}
	existing, err := wp.ListWorkItems(ctx, core.WorkItemFilter{
		Labels: []string{LabelSource, featureLabel(feature.ID)},
		Limit:  1000,
	})
	if err != nil {
		return SyncDraft{}, err
	}
	index := indexExisting(existing)
	plan, err := PlanFeature(ctx, wp, feature)
	if err != nil {
		return SyncDraft{}, err
	}
	return SyncDraft{
		FeatureID: feature.ID,
		Plan:      plan,
		Items:     draftItems(desiredItems(feature, index), index),
		Warnings:  plan.Warnings,
	}, nil
}

func SyncFeature(ctx context.Context, wp core.WorkPlane, feature Feature) (SyncResult, error) {
	return SyncFeatureWithOptions(ctx, wp, feature, SyncOptions{AllowDeletes: true})
}

func SyncFeatureWithOptions(ctx context.Context, wp core.WorkPlane, feature Feature, opts SyncOptions) (SyncResult, error) {
	if wp == nil {
		return SyncResult{}, fmt.Errorf("spec kit sync: WorkPlane is nil")
	}
	if strings.TrimSpace(feature.ID) == "" {
		return SyncResult{}, fmt.Errorf("spec kit sync: feature id is required")
	}
	existing, err := wp.ListWorkItems(ctx, core.WorkItemFilter{
		Labels: []string{LabelSource, featureLabel(feature.ID)},
		Limit:  1000,
	})
	if err != nil {
		return SyncResult{}, err
	}
	index := indexExisting(existing)
	plan, err := PlanFeature(ctx, wp, feature)
	if err != nil {
		return SyncResult{}, err
	}
	if opts.ExpectedHash != "" && opts.ExpectedHash != plan.Hash {
		return SyncResult{}, core.NewAdaptorError(
			core.KindValidation,
			"spec kit sync: stale plan hash %q, current plan hash is %q",
			opts.ExpectedHash,
			plan.Hash,
		)
	}
	if plan.Counts.Delete > 0 && !opts.AllowDeletes {
		return SyncResult{}, core.NewAdaptorError(
			core.KindValidation,
			"spec kit sync: plan deletes %d stale item(s); set allow_deletes to true after reviewing the plan",
			plan.Counts.Delete,
		)
	}
	result := SyncResult{
		FeatureID: feature.ID,
		Stories:   map[string]string{},
		Tasks:     map[string]string{},
		TaskCount: len(feature.Tasks),
		Plan:      plan,
	}
	if len(opts.Items) > 0 {
		return syncDraftItems(ctx, wp, index, plan, feature, opts.Items, result)
	}

	milestone, created, err := upsert(ctx, wp, index, milestoneKey(), milestoneItem(feature))
	if err != nil {
		return SyncResult{}, err
	}
	record(&result, milestone.ID, created)
	result.Milestone = string(milestone.ID)

	epic, created, err := upsert(ctx, wp, index, epicKey(), featureEpicItem(feature, milestone.ID))
	if err != nil {
		return SyncResult{}, err
	}
	record(&result, epic.ID, created)
	result.Epic = string(epic.ID)

	epicDefs := userStoriesForSync(feature)
	for _, us := range epicDefs {
		item := storyItem(feature, us, epic.ID)
		story, created, err := upsert(ctx, wp, index, storyKey(us.ID), item)
		if err != nil {
			return SyncResult{}, err
		}
		record(&result, story.ID, created)
		result.Stories[us.ID] = string(story.ID)
	}

	for _, task := range feature.Tasks {
		parentID := result.Epic
		if task.StoryID != "" && result.Stories[task.StoryID] != "" {
			parentID = result.Stories[task.StoryID]
		} else if result.Stories["US0"] != "" {
			parentID = result.Stories["US0"]
		}
		item := taskItem(feature, task, core.WorkItemID(parentID))
		taskItem, created, err := upsert(ctx, wp, index, taskKey(task.ID), item)
		if err != nil {
			return SyncResult{}, err
		}
		record(&result, taskItem.ID, created)
		result.Tasks[task.ID] = string(taskItem.ID)
	}
	for _, change := range plan.Changes {
		if change.Action != ChangeDelete {
			continue
		}
		deleter, ok := wp.(interface {
			DeleteWorkItem(context.Context, core.WorkItemID) (core.WorkItem, error)
		})
		if !ok {
			return SyncResult{}, fmt.Errorf("spec kit sync: WorkPlane cannot delete stale item %s", change.ID)
		}
		if _, err := deleter.DeleteWorkItem(ctx, core.WorkItemID(change.ID)); err != nil {
			return SyncResult{}, err
		}
		result.Deleted = append(result.Deleted, change.ID)
	}
	result.StoryCount = len(result.Stories)
	sort.Strings(result.Created)
	sort.Strings(result.Updated)
	sort.Strings(result.Deleted)
	return result, nil
}

func syncDraftItems(
	ctx context.Context,
	wp core.WorkPlane,
	index map[string]core.WorkItem,
	plan SyncPlan,
	feature Feature,
	items []core.WorkItem,
	result SyncResult,
) (SyncResult, error) {
	desired := desiredItems(feature, index)
	expected := map[string]desiredItem{}
	for _, d := range desired {
		expected[d.Key] = d
	}
	keyByID := map[core.WorkItemID]string{}
	for _, d := range desired {
		id := d.Item.ID
		if existing, ok := index[d.Key]; ok && existing.ID != "" {
			id = existing.ID
		}
		keyByID[id] = d.Key
	}
	idMap := map[core.WorkItemID]core.WorkItemID{}
	for _, item := range items {
		key := keyByID[item.ID]
		if key == "" {
			return SyncResult{}, core.NewAdaptorError(
				core.KindValidation,
				"spec kit sync: draft item %q is not part of feature %q",
				item.ID,
				feature.ID,
			)
		}
		item = normalizeDraftItem(feature, item, expected[key].Item)
		item.Relationships = remapDraftParents(item.Relationships, idMap)
		out, created, err := upsert(ctx, wp, index, key, item)
		if err != nil {
			return SyncResult{}, err
		}
		idMap[item.ID] = out.ID
		record(&result, out.ID, created)
		switch {
		case key == milestoneKey():
			result.Milestone = string(out.ID)
		case key == epicKey():
			result.Epic = string(out.ID)
		case strings.HasPrefix(key, "story:"):
			if result.Stories == nil {
				result.Stories = map[string]string{}
			}
			result.Stories[strings.TrimPrefix(key, "story:")] = string(out.ID)
		case strings.HasPrefix(key, "task:"):
			if result.Tasks == nil {
				result.Tasks = map[string]string{}
			}
			result.Tasks[strings.TrimPrefix(key, "task:")] = string(out.ID)
		}
	}
	for _, change := range plan.Changes {
		if change.Action != ChangeDelete {
			continue
		}
		deleter, ok := wp.(interface {
			DeleteWorkItem(context.Context, core.WorkItemID) (core.WorkItem, error)
		})
		if !ok {
			return SyncResult{}, fmt.Errorf("spec kit sync: WorkPlane cannot delete stale item %s", change.ID)
		}
		if _, err := deleter.DeleteWorkItem(ctx, core.WorkItemID(change.ID)); err != nil {
			return SyncResult{}, err
		}
		result.Deleted = append(result.Deleted, change.ID)
	}
	result.StoryCount = len(result.Stories)
	sort.Strings(result.Created)
	sort.Strings(result.Updated)
	sort.Strings(result.Deleted)
	return result, nil
}

func remapDraftParents(rels []core.Relationship, idMap map[core.WorkItemID]core.WorkItemID) []core.Relationship {
	if len(rels) == 0 {
		return rels
	}
	out := make([]core.Relationship, len(rels))
	copy(out, rels)
	for i := range out {
		if out[i].Kind != core.RelParentChild {
			continue
		}
		if mapped := idMap[out[i].From]; mapped != "" {
			out[i].From = mapped
		}
		out[i].To = ""
	}
	return out
}

func normalizeDraftItem(feature Feature, item, fallback core.WorkItem) core.WorkItem {
	item.Kind = strings.TrimSpace(item.Kind)
	if item.Kind == "" {
		item.Kind = fallback.Kind
	}
	item.Title = strings.TrimSpace(item.Title)
	if item.Title == "" {
		item.Title = fallback.Title
	}
	if strings.TrimSpace(item.Status) == "" {
		item.Status = fallback.Status
	}
	if item.StateCategory == "" {
		item.StateCategory = fallback.StateCategory
	}
	item.Labels = uniqueStrings(append(item.Labels, requiredTraceLabels(feature.ID, fallback.Labels)...))
	if item.DoD == nil {
		item.DoD = fallback.DoD
	}
	if item.Custom == nil {
		item.Custom = fallback.Custom
	}
	return item
}

func ValidateManifestJSONL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	lines := strings.Split(raw, "\n")
	for i, line := range lines {
		var row struct {
			ID        string `json:"id"`
			IssueType string `json:"issue_type"`
			Title     string `json:"title"`
			Status    string `json:"status"`
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return fmt.Errorf("line %d: %w", i+1, err)
		}
		if strings.TrimSpace(row.ID) == "" {
			return fmt.Errorf("line %d: id is required", i+1)
		}
		if strings.TrimSpace(row.IssueType) == "" {
			return fmt.Errorf("line %d: issue_type is required", i+1)
		}
		if strings.TrimSpace(row.Title) == "" {
			return fmt.Errorf("line %d: title is required", i+1)
		}
		if strings.TrimSpace(row.Status) == "" {
			return fmt.Errorf("line %d: status is required", i+1)
		}
	}
	return nil
}

func indexExisting(items []core.WorkItem) map[string]core.WorkItem {
	out := map[string]core.WorkItem{}
	for _, item := range items {
		for _, label := range item.Labels {
			switch {
			case label == LabelMilestone:
				out[milestoneKey()] = item
			case label == LabelEpic:
				out[epicKey()] = item
			case strings.HasPrefix(label, "speckit:US"):
				out[storyKey(strings.TrimPrefix(label, "speckit:"))] = item
			case strings.HasPrefix(label, "speckit:T"):
				out[taskKey(strings.TrimPrefix(label, "speckit:"))] = item
			}
		}
	}
	return out
}

func draftItems(desired []desiredItem, index map[string]core.WorkItem) []core.WorkItem {
	now := time.Now().UTC()
	out := make([]core.WorkItem, 0, len(desired))
	for _, d := range desired {
		item := d.Item
		if existing, ok := index[d.Key]; ok && existing.ID != "" {
			item.ID = existing.ID
			item.CreatedAt = existing.CreatedAt
		}
		if item.CreatedAt.IsZero() {
			item.CreatedAt = now
		}
		item.UpdatedAt = now
		for i := range item.Relationships {
			if item.Relationships[i].Kind == core.RelParentChild && item.Relationships[i].To == "" {
				item.Relationships[i].To = item.ID
			}
		}
		out = append(out, item)
	}
	return out
}

func requiredTraceLabels(featureID string, fallback []string) []string {
	var out []string
	for _, label := range fallback {
		if label == LabelSource || label == featureLabel(featureID) || strings.HasPrefix(label, "speckit:") {
			out = append(out, label)
		}
	}
	return out
}

func upsert(
	ctx context.Context,
	wp core.WorkPlane,
	index map[string]core.WorkItem,
	key string,
	item core.WorkItem,
) (core.WorkItem, bool, error) {
	if existing, ok := index[key]; ok {
		parent := parentFrom(item.Relationships)
		patch := core.WorkItemPatch{
			Title:         &item.Title,
			Description:   &item.Description,
			Status:        &item.Status,
			StateCategory: &item.StateCategory,
			Labels:        item.Labels,
			DoD:           item.DoD,
			Custom:        item.Custom,
		}
		if parent != "" {
			parentString := string(parent)
			patch.Parent = &parentString
		}
		out, err := wp.UpdateWorkItem(ctx, existing.ID, patch)
		return out, false, err
	}
	out, err := wp.CreateWorkItem(ctx, item)
	return out, true, err
}

func milestoneItem(feature Feature) core.WorkItem {
	return core.WorkItem{
		ID:            syntheticID(feature.ID, "milestone"),
		Kind:          core.KindMilestone,
		Title:         feature.Title,
		Description:   milestoneDescription(feature),
		Status:        "open",
		StateCategory: core.StateBacklog,
		Labels:        baseLabels(feature.ID, LabelMilestone),
		DoD:           dod(feature.Spec.AcceptanceScenarios, feature.Spec.FunctionalRequirements),
		Custom:        custom(feature.ID, "milestone", ""),
	}
}

func featureEpicItem(feature Feature, parent core.WorkItemID) core.WorkItem {
	return core.WorkItem{
		ID:            syntheticID(feature.ID, "epic"),
		Kind:          "epic",
		Title:         "Feature: " + feature.Title,
		Description:   featureEpicDescription(feature),
		Status:        "open",
		StateCategory: core.StateBacklog,
		Labels:        baseLabels(feature.ID, LabelEpic),
		Relationships: []core.Relationship{{Kind: core.RelParentChild, From: parent, To: ""}},
		DoD:           dod(feature.Spec.AcceptanceScenarios, feature.Spec.FunctionalRequirements),
		Custom:        custom(feature.ID, "epic", ""),
	}
}

func storyItem(feature Feature, story UserStory, parent core.WorkItemID) core.WorkItem {
	title := story.Title
	if title == "" {
		title = "Feature story"
	}
	if story.ID != "US0" {
		title = story.ID + ": " + title
	}
	labels := baseLabels(feature.ID, LabelStory, "speckit:"+story.ID)
	return core.WorkItem{
		ID:            syntheticID(feature.ID, story.ID),
		Kind:          "story",
		Title:         title,
		Description:   storyDescription(feature, story),
		Status:        "open",
		StateCategory: core.StateBacklog,
		Labels:        labels,
		Relationships: []core.Relationship{{Kind: core.RelParentChild, From: parent, To: ""}},
		DoD:           dod(story.AcceptanceScenarios, nil),
		Custom:        custom(feature.ID, "story", story.ID),
	}
}

func taskItem(feature Feature, task Task, parent core.WorkItemID) core.WorkItem {
	title := strings.TrimSpace(task.Title)
	if title == "" {
		title = task.ID
	} else {
		title = task.ID + ": " + title
	}
	labels := baseLabels(feature.ID, LabelTask, "speckit:"+task.ID)
	if task.StoryID != "" {
		labels = append(labels, "speckit:"+task.StoryID)
	}
	if task.Parallel {
		labels = append(labels, "speckit:parallel")
	}
	return core.WorkItem{
		ID:            syntheticID(feature.ID, task.ID),
		Kind:          "task",
		Title:         title,
		Description:   taskDescription(feature, task),
		Status:        "open",
		StateCategory: core.StateBacklog,
		Labels:        labels,
		Relationships: []core.Relationship{{Kind: core.RelParentChild, From: parent, To: ""}},
		Custom:        custom(feature.ID, "task", task.ID),
		DoD: &core.DefinitionOfDone{
			AcceptanceCriteria: []string{"Complete Spec Kit task " + task.ID + ": " + strings.TrimSpace(task.Title)},
			Version:            "spec-kit:v1",
		},
	}
}

func custom(featureID, kind, sourceID string) map[string]any {
	out := map[string]any{
		"speckit.feature_id": featureID,
		"speckit.kind":       kind,
	}
	switch kind {
	case "story":
		out["speckit.user_story_id"] = sourceID
	case "task":
		out["speckit.task_id"] = sourceID
	}
	return out
}

func userStoriesForSync(feature Feature) []UserStory {
	if len(feature.Spec.UserStories) > 0 {
		return feature.Spec.UserStories
	}
	return []UserStory{{
		ID:    "US0",
		Title: feature.Title,
	}}
}

func desiredItems(feature Feature, index map[string]core.WorkItem) []desiredItem {
	milestone := milestoneItem(feature)
	milestoneID := itemID(index, milestoneKey(), milestone.ID)
	epic := featureEpicItem(feature, milestoneID)
	epicID := itemID(index, epicKey(), epic.ID)
	out := []desiredItem{
		{Key: milestoneKey(), Item: milestone},
		{Key: epicKey(), Item: epic},
	}
	for _, us := range userStoriesForSync(feature) {
		story := storyItem(feature, us, epicID)
		out = append(out, desiredItem{Key: storyKey(us.ID), SourceID: us.ID, Item: story})
	}
	for _, task := range feature.Tasks {
		parentID := epicID
		if task.StoryID != "" {
			parentID = itemID(index, storyKey(task.StoryID), syntheticID(feature.ID, task.StoryID))
		} else if len(feature.Spec.UserStories) == 0 {
			parentID = itemID(index, storyKey("US0"), syntheticID(feature.ID, "US0"))
		}
		out = append(out, desiredItem{
			Key:      taskKey(task.ID),
			SourceID: task.ID,
			Item:     taskItem(feature, task, parentID),
		})
	}
	return out
}

func itemID(index map[string]core.WorkItem, key string, fallback core.WorkItemID) core.WorkItemID {
	if item, ok := index[key]; ok && item.ID != "" {
		return item.ID
	}
	return fallback
}

func (p *SyncPlan) addChange(action ChangeKind, key, sourceID string, id core.WorkItemID, kind, title string) {
	p.Changes = append(p.Changes, SyncChange{
		Action:   action,
		Key:      key,
		Kind:     kind,
		SourceID: sourceID,
		ID:       string(id),
		Title:    title,
		Summary:  changeSummary(action, kind, title),
	})
	switch action {
	case ChangeCreate:
		p.Counts.Create++
	case ChangeUpdate:
		p.Counts.Update++
	case ChangeDelete:
		p.Counts.Delete++
	}
}

func (p *SyncPlan) finalize() {
	sort.SliceStable(p.Changes, func(i, j int) bool {
		a, b := p.Changes[i], p.Changes[j]
		if actionRank(a.Action) != actionRank(b.Action) {
			return actionRank(a.Action) < actionRank(b.Action)
		}
		if a.Key != b.Key {
			return a.Key < b.Key
		}
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		return a.Title < b.Title
	})
	type hashablePlan struct {
		FeatureID string       `json:"feature_id"`
		Changes   []SyncChange `json:"changes"`
		Counts    ChangeCounts `json:"counts"`
		JSONL     string       `json:"jsonl,omitempty"`
		Warnings  []string     `json:"warnings,omitempty"`
	}
	payload, _ := json.Marshal(hashablePlan{
		FeatureID: p.FeatureID,
		Changes:   p.Changes,
		Counts:    p.Counts,
		JSONL:     p.JSONL,
		Warnings:  p.Warnings,
	})
	sum := sha256.Sum256(payload)
	p.Hash = "sha256:" + hex.EncodeToString(sum[:])
}

func actionRank(action ChangeKind) int {
	switch action {
	case ChangeCreate:
		return 0
	case ChangeUpdate:
		return 1
	case ChangeDelete:
		return 2
	default:
		return 9
	}
}

func changeSummary(action ChangeKind, kind, title string) string {
	return fmt.Sprintf("%s %s %q", action, kind, title)
}

func sourceIDFromKey(key string) string {
	if strings.Contains(key, ":") {
		return key[strings.Index(key, ":")+1:]
	}
	return ""
}

func ManifestJSONL(items []desiredItem) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	for _, d := range items {
		row := map[string]any{
			"id":             string(d.Item.ID),
			"issue_type":     d.Item.Kind,
			"title":          d.Item.Title,
			"description":    d.Item.Description,
			"status":         d.Item.Status,
			"state_category": d.Item.StateCategory,
			"labels":         d.Item.Labels,
		}
		if len(d.Item.Custom) > 0 {
			if raw, err := json.Marshal(d.Item.Custom); err == nil {
				row["metadata"] = string(raw)
			}
		}
		if d.Item.DoD != nil {
			row["acceptance"] = d.Item.DoD.AcceptanceCriteria
		}
		if parent := parentFrom(d.Item.Relationships); parent != "" {
			row["dependencies"] = []map[string]any{{
				"issue_id":      string(d.Item.ID),
				"depends_on_id": string(parent),
				"type":          "parent-child",
				"metadata":      "{}",
			}}
		}
		_ = enc.Encode(row)
	}
	return strings.TrimRight(b.String(), "\n")
}

func baseLabels(featureID string, extra ...string) []string {
	labels := []string{LabelSource, featureLabel(featureID)}
	labels = append(labels, extra...)
	return uniqueStrings(labels)
}

func featureLabel(featureID string) string { return "speckit:" + safeLabelPart(featureID) }

func safeLabelPart(v string) string {
	v = strings.TrimSpace(v)
	v = strings.ReplaceAll(v, " ", "-")
	v = strings.ReplaceAll(v, string(filepath.Separator), "-")
	return v
}

func syntheticID(featureID, part string) core.WorkItemID {
	return core.WorkItemID("speckit/" + safeLabelPart(featureID) + "/" + safeLabelPart(part))
}

func milestoneKey() string { return "milestone" }
func epicKey() string      { return "epic" }
func storyKey(id string) string {
	if id == "" {
		return ""
	}
	return "story:" + id
}
func taskKey(id string) string {
	if id == "" {
		return ""
	}
	return "task:" + id
}

func parentFrom(rels []core.Relationship) core.WorkItemID {
	for _, rel := range rels {
		if rel.Kind == core.RelParentChild && rel.From != "" {
			return rel.From
		}
	}
	return ""
}

func dod(primary, fallback []string) *core.DefinitionOfDone {
	criteria := primary
	if len(criteria) == 0 {
		criteria = fallback
	}
	if len(criteria) == 0 {
		return nil
	}
	return &core.DefinitionOfDone{
		AcceptanceCriteria: uniqueStrings(criteria),
		Version:            "spec-kit:v1",
	}
}

func milestoneDescription(feature Feature) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Spec Kit feature `%s` synced into Beads as a milestone.\n\n", feature.ID)
	writeArtifacts(&b, feature)
	if len(feature.Spec.UserStories) > 0 {
		b.WriteString("\nUser stories:\n")
		for _, us := range feature.Spec.UserStories {
			if us.Priority != "" {
				fmt.Fprintf(&b, "- %s: %s (%s)\n", us.ID, us.Title, us.Priority)
			} else {
				fmt.Fprintf(&b, "- %s: %s\n", us.ID, us.Title)
			}
		}
	}
	return strings.TrimSpace(b.String())
}

func featureEpicDescription(feature Feature) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Spec Kit feature `%s` synced into Beads as an epic.", feature.ID)
	if len(feature.Spec.FunctionalRequirements) > 0 {
		b.WriteString("\n\nFunctional requirements:\n")
		for _, req := range feature.Spec.FunctionalRequirements {
			fmt.Fprintf(&b, "- %s\n", req)
		}
	}
	return strings.TrimSpace(b.String())
}

func storyDescription(feature Feature, story UserStory) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Spec Kit user story `%s` for feature `%s`.\n", story.ID, feature.ID)
	if story.Priority != "" {
		fmt.Fprintf(&b, "\nPriority: %s\n", story.Priority)
	}
	if len(story.AcceptanceScenarios) > 0 {
		b.WriteString("\nAcceptance scenarios:\n")
		for _, scenario := range story.AcceptanceScenarios {
			fmt.Fprintf(&b, "- %s\n", scenario)
		}
	}
	return strings.TrimSpace(b.String())
}

func taskDescription(feature Feature, task Task) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Spec Kit task `%s` for feature `%s`.", task.ID, feature.ID)
	if task.StoryID != "" {
		fmt.Fprintf(&b, "\n\nUser story: %s", task.StoryID)
	}
	if task.Phase != "" {
		fmt.Fprintf(&b, "\nPhase: %s", task.Phase)
	}
	if task.Line > 0 && feature.TasksPath != "" {
		fmt.Fprintf(&b, "\nSource: %s:%d", feature.TasksPath, task.Line)
	}
	if task.Parallel {
		b.WriteString("\nParallel-safe: yes")
	}
	return strings.TrimSpace(b.String())
}

func writeArtifacts(b *strings.Builder, feature Feature) {
	b.WriteString("Artifacts:\n")
	if feature.SpecPath != "" {
		fmt.Fprintf(b, "- spec: %s\n", feature.SpecPath)
	}
	if feature.PlanPath != "" {
		fmt.Fprintf(b, "- plan: %s\n", feature.PlanPath)
	}
	if feature.TasksPath != "" {
		fmt.Fprintf(b, "- tasks: %s\n", feature.TasksPath)
	}
}

func record(result *SyncResult, id core.WorkItemID, created bool) {
	if created {
		result.Created = append(result.Created, string(id))
	} else {
		result.Updated = append(result.Updated, string(id))
	}
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}
