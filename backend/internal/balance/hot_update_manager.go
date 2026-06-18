package balance

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	pb "github.com/ecscard/game/internal/proto"
	game_components "github.com/ecscard/game/internal/game/components"
)

type OverrideValue struct {
	Value     float64 `bson:"value"`
	UpdatedAt int64   `bson:"updated_at"`
}

type CardOverride struct {
	TemplateID    string                   `bson:"template_id"`
	Overrides     map[string]*OverrideValue `bson:"overrides"`
	ChangeReason  string                   `bson:"change_reason"`
	ChangedBy     string                   `bson:"changed_by"`
	EffectiveFrom int64                    `bson:"effective_from"`
	IsActive      bool                     `bson:"is_active"`
	ChangeID      string                   `bson:"change_id"`
}

type ChangeLogDocument struct {
	ID                primitive.ObjectID      `bson:"_id,omitempty"`
	ChangeID          string                  `bson:"change_id"`
	TemplateID        string                  `bson:"template_id"`
	CardName          string                  `bson:"card_name,omitempty"`
	TimestampUnixMs   int64                   `bson:"timestamp_unix_ms"`
	ChangedBy         string                  `bson:"changed_by"`
	ChangeReason      string                  `bson:"change_reason"`
	ValuesBefore      map[string]float64      `bson:"values_before"`
	ValuesAfter       map[string]float64      `bson:"values_after"`
	PredictedImpact   float64                 `bson:"predicted_impact"`
	ActualImpact24H   float64                 `bson:"actual_impact_24h,omitempty"`
}

type HotUpdateManager struct {
	mu              sync.RWMutex
	db              *mongo.Database
	overrideCol     *mongo.Collection
	changeLogCol    *mongo.Collection
	activeOverrides map[string]*CardOverride
	changeCount     atomic.Int64
	updateListeners []func(string)
}

func NewHotUpdateManager(mongoURI, dbName string) (*HotUpdateManager, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		return nil, fmt.Errorf("connect mongo: %w", err)
	}

	if err := client.Ping(ctx, nil); err != nil {
		return nil, fmt.Errorf("ping mongo: %w", err)
	}

	db := client.Database(dbName)
	overrideCol := db.Collection("card_overrides")
	changeLogCol := db.Collection("balance_change_logs")

	_, err = overrideCol.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "template_id", Value: 1}}},
		{Keys: bson.D{{Key: "is_active", Value: 1}}},
	})
	if err != nil {
		return nil, fmt.Errorf("create override indexes: %w", err)
	}

	_, err = changeLogCol.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "change_id", Value: 1}, {Key: "template_id", Value: -1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "template_id", Value: 1}}},
		{Keys: bson.D{{Key: "timestamp_unix_ms", Value: -1}}},
	})
	if err != nil {
		return nil, fmt.Errorf("create changelog indexes: %w", err)
	}

	hm := &HotUpdateManager{
		db:              db,
		overrideCol:     overrideCol,
		changeLogCol:    changeLogCol,
		activeOverrides: make(map[string]*CardOverride),
	}

	if err := hm.LoadOverridesFromDB(); err != nil {
		return nil, fmt.Errorf("load overrides: %w", err)
	}

	return hm, nil
}

func (hm *HotUpdateManager) ApplyHotUpdate(templateID string, overrides map[string]float64, reason, changedBy string, immediate bool) (*pb.CardBalanceChangeLog, error) {
	if len(overrides) == 0 {
		return nil, fmt.Errorf("no overrides provided")
	}

	hm.mu.Lock()
	defer hm.mu.Unlock()

	now := time.Now()
	nowMs := now.UnixMilli()

	var effectiveFrom int64
	if immediate {
		effectiveFrom = nowMs
	} else {
		nextMidnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
		effectiveFrom = nextMidnight.UnixMilli()
	}

	existing, exists := hm.activeOverrides[templateID]

	beforeValues := make(map[string]float64)
	afterValues := make(map[string]float64)
	mergedOverrides := make(map[string]*OverrideValue)

	if exists {
		for k, v := range existing.Overrides {
			mergedOverrides[k] = v
			beforeValues[k] = v.Value
		}
	}

	for field, val := range overrides {
		if existingVal, ok := mergedOverrides[field]; ok {
			beforeValues[field] = existingVal.Value
		}
		mergedOverrides[field] = &OverrideValue{
			Value:     val,
			UpdatedAt: nowMs,
		}
		afterValues[field] = val
	}

	changeID := uuid.New().String()

	newOverride := &CardOverride{
		TemplateID:    templateID,
		Overrides:     mergedOverrides,
		ChangeReason:  reason,
		ChangedBy:     changedBy,
		EffectiveFrom: effectiveFrom,
		IsActive:      true,
		ChangeID:      changeID,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := hm.overrideCol.ReplaceOne(ctx,
		bson.M{"template_id": templateID},
		newOverride,
		options.Replace().SetUpsert(true),
	)
	if err != nil {
		return nil, fmt.Errorf("save override: %w", err)
	}

	predictedImpact := hm.PredictImpact(beforeValues, afterValues)

	changeLog := hm.buildChangeLog(changeID, templateID, beforeValues, afterValues, reason, changedBy, nil)
	changeLog.PredictedImpact = predictedImpact

	doc := &ChangeLogDocument{
		ChangeID:        changeID,
		TemplateID:      templateID,
		CardName:        changeLog.CardName,
		TimestampUnixMs: changeLog.TimestampUnixMs,
		ChangedBy:       changeLog.ChangedBy,
		ChangeReason:    changeLog.ChangeReason,
		ValuesBefore:    changeLog.ValuesBefore,
		ValuesAfter:     changeLog.ValuesAfter,
		PredictedImpact: changeLog.PredictedImpact,
	}

	_, err = hm.changeLogCol.InsertOne(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("save change log: %w", err)
	}

	if immediate {
		hm.activeOverrides[templateID] = newOverride
	}

	hm.changeCount.Add(1)

	hm.notifyListeners(templateID)

	return changeLog, nil
}

func (hm *HotUpdateManager) RevertHotUpdate(changeID, revertedBy string) (string, error) {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var doc ChangeLogDocument
	err := hm.changeLogCol.FindOne(ctx, bson.M{"change_id": changeID}).Decode(&doc)
	if err != nil {
		return "", fmt.Errorf("find change log: %w", err)
	}

	revertedBefore := make(map[string]float64)
	revertedAfter := make(map[string]float64)
	for k, v := range doc.ValuesAfter {
		revertedBefore[k] = v
	}
	for k, v := range doc.ValuesBefore {
		revertedAfter[k] = v
	}

	existing, exists := hm.activeOverrides[doc.TemplateID]
	mergedOverrides := make(map[string]*OverrideValue)
	if exists {
		for k, v := range existing.Overrides {
			mergedOverrides[k] = v
		}
	}

	nowMs := time.Now().UnixMilli()
	for field, val := range revertedAfter {
		if val == 0 {
			delete(mergedOverrides, field)
		} else {
			mergedOverrides[field] = &OverrideValue{
				Value:     val,
				UpdatedAt: nowMs,
			}
		}
	}

	revertChangeID := uuid.New().String()
	revertReason := fmt.Sprintf("Revert of %s: %s", changeID, doc.ChangeReason)

	newOverride := &CardOverride{
		TemplateID:    doc.TemplateID,
		Overrides:     mergedOverrides,
		ChangeReason:  revertReason,
		ChangedBy:     revertedBy,
		EffectiveFrom: nowMs,
		IsActive:      len(mergedOverrides) > 0,
		ChangeID:      revertChangeID,
	}

	_, err = hm.overrideCol.ReplaceOne(ctx,
		bson.M{"template_id": doc.TemplateID},
		newOverride,
		options.Replace().SetUpsert(true),
	)
	if err != nil {
		return "", fmt.Errorf("save reverted override: %w", err)
	}

	predictedImpact := hm.PredictImpact(revertedBefore, revertedAfter)

	revertDoc := &ChangeLogDocument{
		ChangeID:        revertChangeID,
		TemplateID:      doc.TemplateID,
		CardName:        doc.CardName,
		TimestampUnixMs: nowMs,
		ChangedBy:       revertedBy,
		ChangeReason:    revertReason,
		ValuesBefore:    revertedBefore,
		ValuesAfter:     revertedAfter,
		PredictedImpact: predictedImpact,
	}

	_, err = hm.changeLogCol.InsertOne(ctx, revertDoc)
	if err != nil {
		return "", fmt.Errorf("save revert log: %w", err)
	}

	if len(mergedOverrides) > 0 {
		hm.activeOverrides[doc.TemplateID] = newOverride
	} else {
		delete(hm.activeOverrides, doc.TemplateID)
	}

	hm.changeCount.Add(1)
	hm.notifyListeners(doc.TemplateID)

	return revertChangeID, nil
}

func (hm *HotUpdateManager) GetActiveOverrides(templateID string) []*pb.CardTemplateOverride {
	hm.mu.RLock()
	defer hm.mu.RUnlock()

	var result []*pb.CardTemplateOverride

	if templateID != "" {
		if co, ok := hm.activeOverrides[templateID]; ok {
			result = append(result, hm.toProtoOverride(co))
		}
		return result
	}

	for _, co := range hm.activeOverrides {
		result = append(result, hm.toProtoOverride(co))
	}
	return result
}

func (hm *HotUpdateManager) toProtoOverride(co *CardOverride) *pb.CardTemplateOverride {
	numericOverrides := make(map[string]float64)
	for k, v := range co.Overrides {
		numericOverrides[k] = v.Value
	}
	return &pb.CardTemplateOverride{
		TemplateId:          co.TemplateID,
		NumericOverrides:    numericOverrides,
		ChangeReason:        co.ChangeReason,
		ChangedBy:           co.ChangedBy,
		EffectiveFromUnixMs: co.EffectiveFrom,
		IsActive:            co.IsActive,
	}
}

func (hm *HotUpdateManager) GetOverrideValue(templateID, field string) (float64, bool) {
	hm.mu.RLock()
	defer hm.mu.RUnlock()

	co, ok := hm.activeOverrides[templateID]
	if !ok {
		return 0, false
	}
	ov, ok := co.Overrides[field]
	if !ok {
		return 0, false
	}
	return ov.Value, true
}

func (hm *HotUpdateManager) OverrideCardComponent(templateID string, comp *game_components.CardComponent) {
	if comp == nil {
		return
	}
	if val, ok := hm.GetOverrideValue(templateID, "cost"); ok {
		comp.Cost = int(val)
	}
}

func (hm *HotUpdateManager) OverrideMinionComponent(templateID string, comp *game_components.MinionComponent) {
	if comp == nil {
		return
	}
	if val, ok := hm.GetOverrideValue(templateID, "attack"); ok {
		comp.Attack = int(val)
	}
	if val, ok := hm.GetOverrideValue(templateID, "health"); ok {
		comp.Health = int(val)
		comp.MaxHealth = int(val)
	}
	if val, ok := hm.GetOverrideValue(templateID, "taunt"); ok {
		comp.Taunt = val != 0
	}
	if val, ok := hm.GetOverrideValue(templateID, "charge"); ok {
		comp.Charge = val != 0
		comp.CanAttack = comp.CanAttack || comp.Charge
	}
	if val, ok := hm.GetOverrideValue(templateID, "divine_shield"); ok {
		comp.DivineShield = val != 0
	}
}

func (hm *HotUpdateManager) OverrideSpellComponent(templateID string, comp *game_components.SpellComponent) {
	if comp == nil {
		return
	}
	if val, ok := hm.GetOverrideValue(templateID, "spell_value"); ok {
		comp.Value = int(val)
	}
}

func (hm *HotUpdateManager) OverrideWeaponComponent(templateID string, comp *game_components.WeaponComponent) {
	if comp == nil {
		return
	}
	if val, ok := hm.GetOverrideValue(templateID, "attack"); ok {
		comp.Attack = int(val)
	}
	if val, ok := hm.GetOverrideValue(templateID, "durability"); ok {
		comp.Durability = int(val)
		comp.MaxDurability = int(val)
	}
}

func (hm *HotUpdateManager) OverrideHeroComponent(templateID string, comp *game_components.HeroComponent) {
	if comp == nil {
		return
	}
	if val, ok := hm.GetOverrideValue(templateID, "armor"); ok {
		comp.Armor = int(val)
	}
}

func (hm *HotUpdateManager) GetChangeHistory(templateID string, limit int32, fromMs, toMs int64) ([]*pb.CardBalanceChangeLog, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	filter := bson.M{}
	if templateID != "" {
		filter["template_id"] = templateID
	}
	if fromMs > 0 || toMs > 0 {
		timeFilter := bson.M{}
		if fromMs > 0 {
			timeFilter["$gte"] = fromMs
		}
		if toMs > 0 {
			timeFilter["$lte"] = toMs
		}
		if len(timeFilter) > 0 {
			filter["timestamp_unix_ms"] = timeFilter
		}
	}

	opts := options.Find().SetSort(bson.M{"timestamp_unix_ms": -1})
	if limit > 0 {
		opts.SetLimit(int64(limit))
	}

	cursor, err := hm.changeLogCol.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("find changes: %w", err)
	}
	defer cursor.Close(ctx)

	var result []*pb.CardBalanceChangeLog
	for cursor.Next(ctx) {
		var doc ChangeLogDocument
		if err := cursor.Decode(&doc); err != nil {
			continue
		}
		result = append(result, &pb.CardBalanceChangeLog{
			ChangeId:        doc.ChangeID,
			TemplateId:      doc.TemplateID,
			CardName:        doc.CardName,
			TimestampUnixMs: doc.TimestampUnixMs,
			ChangedBy:       doc.ChangedBy,
			ChangeReason:    doc.ChangeReason,
			ValuesBefore:    doc.ValuesBefore,
			ValuesAfter:     doc.ValuesAfter,
			PredictedImpact: doc.PredictedImpact,
			ActualImpact24H: doc.ActualImpact24H,
		})
	}
	return result, nil
}

func (hm *HotUpdateManager) LoadOverridesFromDB() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	nowMs := time.Now().UnixMilli()
	filter := bson.M{
		"is_active":      true,
		"effective_from": bson.M{"$lte": nowMs},
	}

	cursor, err := hm.overrideCol.Find(ctx, filter)
	if err != nil {
		return fmt.Errorf("find overrides: %w", err)
	}
	defer cursor.Close(ctx)

	for cursor.Next(ctx) {
		var co CardOverride
		if err := cursor.Decode(&co); err != nil {
			continue
		}
		hm.activeOverrides[co.TemplateID] = &co
	}

	return nil
}

func (hm *HotUpdateManager) AddUpdateListener(fn func(templateID string)) {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	hm.updateListeners = append(hm.updateListeners, fn)
}

func (hm *HotUpdateManager) notifyListeners(templateID string) {
	for _, fn := range hm.updateListeners {
		fn(templateID)
	}
}

func (hm *HotUpdateManager) buildChangeLog(changeID, templateID string, before, after map[string]float64, reason, changedBy string, statsBefore *pb.CardBalanceStats) *pb.CardBalanceChangeLog {
	return &pb.CardBalanceChangeLog{
		ChangeId:        changeID,
		TemplateId:      templateID,
		TimestampUnixMs: time.Now().UnixMilli(),
		ChangedBy:       changedBy,
		ChangeReason:    reason,
		ValuesBefore:    before,
		ValuesAfter:     after,
		StatsBefore:     statsBefore,
	}
}

func (hm *HotUpdateManager) PredictImpact(before, after map[string]float64) float64 {
	var impact float64
	totalChanges := 0

	for field, afterVal := range after {
		beforeVal := before[field]
		delta := afterVal - beforeVal

		var weight float64
		switch field {
		case "cost":
			weight = -5.0
		case "attack":
			weight = 2.5
		case "health":
			weight = 2.0
		case "durability":
			weight = 1.5
		case "spell_value":
			weight = 3.0
		case "taunt", "charge", "divine_shield":
			weight = 10.0
		case "armor":
			weight = 1.0
		default:
			weight = 1.0
		}

		impact += delta * weight
		totalChanges++
	}

	if totalChanges == 0 {
		return 0
	}
	return impact
}

func (hm *HotUpdateManager) Close(ctx context.Context) error {
	return hm.db.Client().Disconnect(ctx)
}
