package lockstep

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sync"
	"time"

	"github.com/ecscard/game/internal/ecs"
	"github.com/ecscard/game/internal/game/components"
	pb "github.com/ecscard/game/internal/proto"
	"github.com/mitchellh/hashstructure/v2"
)

const (
	SnapshotIntervalMs = 100
	MaxSnapshotHistory = 128
	MaxPendingActions  = 1024
)

type GameSnapshot struct {
	FrameNumber  uint64
	Hash         uint64
	Status       *pb.GameStatus
	PendingActions []*pb.Action
	CreatedAt    int64
	Checksum     uint32
}

type LockstepEngine struct {
	mu sync.RWMutex

	gameID string

	currentFrame    uint64
	currentSnapshot *GameSnapshot
	snapshots       map[uint64]*GameSnapshot
	snapshotOrder   []uint64

	pendingActions   map[string]map[uint64]*pb.Action
	playerSequences  map[string]uint64
	playerAcks       map[string]uint64
	playerHashChecks map[string]uint64

	entityCounter    uint64
	desyncThreshold  int
	desyncCount      int

	onRollback    func(snapshot *GameSnapshot, reason pb.RollbackReason)
	onSnapshot    func(snapshot *GameSnapshot)
	onDesync      func(playerID string, expected, actual uint64)

	world          *ecs.World
	serializer     *StateSerializer
}

type StateSerializer struct{}

func NewLockstepEngine(gameID string, world *ecs.World) *LockstepEngine {
	return &LockstepEngine{
		gameID:         gameID,
		currentFrame:   0,
		snapshots:      make(map[uint64]*GameSnapshot),
		snapshotOrder:  make([]uint64, 0, MaxSnapshotHistory),
		pendingActions: make(map[string]map[uint64]*pb.Action),
		playerSequences:  make(map[string]uint64),
		playerAcks:       make(map[string]uint64),
		playerHashChecks: make(map[string]uint64),
		desyncThreshold:  3,
		world:            world,
		serializer:       &StateSerializer{},
	}
}

func (e *LockstepEngine) SetCallbacks(
	onRollback func(*GameSnapshot, pb.RollbackReason),
	onSnapshot func(*GameSnapshot),
	onDesync func(string, uint64, uint64),
) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.onRollback = onRollback
	e.onSnapshot = onSnapshot
	e.onDesync = onDesync
}

func (e *LockstepEngine) RegisterPlayer(playerID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pendingActions[playerID] = make(map[uint64]*pb.Action)
	e.playerSequences[playerID] = 0
	e.playerAcks[playerID] = 0
	e.playerHashChecks[playerID] = 0
}

func (e *LockstepEngine) ValidateAction(action *pb.Action) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	playerSeq, ok := e.playerSequences[action.PlayerId]
	if !ok {
		return false
	}

	if action.Sequence != playerSeq+1 {
		return false
	}

	if e.currentSnapshot != nil {
		if action.BaseSnapshotFrame > e.currentSnapshot.FrameNumber {
			return false
		}
		if action.BaseSnapshotFrame == e.currentSnapshot.FrameNumber {
			if action.BaseSnapshotHash != 0 && action.BaseSnapshotHash != e.currentSnapshot.Hash {
				return false
			}
		}
	}

	return true
}

func (e *LockstepEngine) SubmitAction(action *pb.Action) (bool, *GameSnapshot) {
	if !e.ValidateAction(action) {
		e.mu.RLock()
		snap := e.currentSnapshot
		e.mu.RUnlock()
		return false, snap
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	e.playerSequences[action.PlayerId] = action.Sequence

	playerActions, _ := e.pendingActions[action.PlayerId]
	playerActions[action.Sequence] = action

	return true, nil
}

func (e *LockstepEngine) GenerateSnapshot(status *pb.GameStatus) *GameSnapshot {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.currentFrame++
	frame := e.currentFrame

	allPending := make([]*pb.Action, 0)
	for _, playerActions := range e.pendingActions {
		for _, a := range playerActions {
			allPending = append(allPending, a)
		}
	}

	status.FrameNumber = frame
	status.SnapshotFrame = frame
	status.Timestamp = time.Now().UnixNano() / int64(time.Millisecond)

	hash := e.computeStateHash(status, allPending, frame)

	checksum := e.computeChecksum(frame, hash, status)

	snap := &GameSnapshot{
		FrameNumber:    frame,
		Hash:           hash,
		Status:         status,
		PendingActions: allPending,
		CreatedAt:      time.Now().UnixNano(),
		Checksum:       checksum,
	}

	status.SnapshotHash = hash

	e.currentSnapshot = snap
	e.snapshots[frame] = snap
	e.snapshotOrder = append(e.snapshotOrder, frame)

	if len(e.snapshotOrder) > MaxSnapshotHistory {
		oldest := e.snapshotOrder[0]
		e.snapshotOrder = e.snapshotOrder[1:]
		delete(e.snapshots, oldest)
	}

	for pid := range e.pendingActions {
		e.pendingActions[pid] = make(map[uint64]*pb.Action)
	}

	if e.onSnapshot != nil {
		go e.onSnapshot(snap)
	}

	return snap
}

func (e *LockstepEngine) GetSnapshot(frame uint64) (*GameSnapshot, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	s, ok := e.snapshots[frame]
	return s, ok
}

func (e *LockstepEngine) GetCurrentSnapshot() *GameSnapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.currentSnapshot
}

func (e *LockstepEngine) GetLatestSnapshotFrame() uint64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.currentSnapshot == nil {
		return 0
	}
	return e.currentSnapshot.FrameNumber
}

func (e *LockstepEngine) ReceiveAck(ack *pb.FrameAck) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if ack.FrameNumber > e.playerAcks[ack.PlayerId] {
		e.playerAcks[ack.PlayerId] = ack.FrameNumber
	}

	if e.currentSnapshot != nil && ack.FrameNumber == e.currentSnapshot.FrameNumber {
		if ack.ExpectedHash != 0 && ack.ExpectedHash != e.currentSnapshot.Hash {
			e.desyncCount++
			if e.onDesync != nil {
				go e.onDesync(ack.PlayerId, e.currentSnapshot.Hash, ack.ExpectedHash)
			}
			if e.desyncCount >= e.desyncThreshold && e.onRollback != nil {
				go e.onRollback(e.currentSnapshot, pb.RollbackReason_ROLLBACK_REASON_DESYNC)
			}
		} else {
			e.playerHashChecks[ack.PlayerId] = ack.FrameNumber
		}
	}
}

func (e *LockstepEngine) RequestRollback(targetFrame uint64, reason pb.RollbackReason) (*GameSnapshot, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	snap, ok := e.snapshots[targetFrame]
	if !ok {
		if len(e.snapshotOrder) > 0 {
			oldest := e.snapshotOrder[0]
			snap = e.snapshots[oldest]
			return snap, true
		}
		return nil, false
	}

	if e.onRollback != nil {
		go e.onRollback(snap, reason)
	}

	return snap, true
}

func (e *LockstepEngine) RestoreFromSnapshot(snap *GameSnapshot) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.currentFrame = snap.FrameNumber
	e.currentSnapshot = snap

	e.desyncCount = 0
}

func (e *LockstepEngine) GetPlayerAck(playerID string) uint64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.playerAcks[playerID]
}

func (e *LockstepEngine) GetAllPlayerAcks() map[string]uint64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	result := make(map[string]uint64, len(e.playerAcks))
	for k, v := range e.playerAcks {
		result[k] = v
	}
	return result
}

func (e *LockstepEngine) computeStateHash(
	status *pb.GameStatus,
	actions []*pb.Action,
	frame uint64,
) uint64 {
	hashVal, err := hashstructure.Hash(map[string]interface{}{
		"frame":        frame,
		"turn":         status.Turn,
		"currentPlayer": status.CurrentTurnPlayerId,
		"state":        status.State,
		"players":      status.Players,
		"cards":        status.Cards,
		"winner":       status.Winner,
		"actions":      actions,
	}, hashstructure.FormatV2, nil)

	if err != nil {
		h := sha256.New()
		buf := make([]byte, 8)
		binary.LittleEndian.PutUint64(buf, frame)
		h.Write(buf)
		binary.LittleEndian.PutUint32(buf[:4], uint32(status.Turn))
		h.Write(buf[:4])
		sum := h.Sum(nil)
		return binary.LittleEndian.Uint64(sum[:8])
	}

	return hashVal
}

func (e *LockstepEngine) computeChecksum(
	frame uint64,
	hash uint64,
	status *pb.GameStatus,
) uint32 {
	var sum uint32
	sum += uint32(frame)
	sum += uint32(frame >> 32)
	sum += uint32(hash)
	sum += uint32(hash >> 32)
	sum += uint32(status.Turn) * 31
	sum += uint32(len(status.Players)) * 37
	sum += uint32(len(status.Cards)) * 41
	return sum
}

func (e *LockstepEngine) ConvertToProtoSnapshot(snap *GameSnapshot) *pb.GameSnapshot {
	if snap == nil {
		return nil
	}
	return &pb.GameSnapshot{
		FrameNumber:    snap.FrameNumber,
		Hash:           snap.Hash,
		Status:         snap.Status,
		PendingActions: snap.PendingActions,
		CreatedAt:      snap.CreatedAt / int64(time.Millisecond),
		Checksum:       snap.Checksum,
	}
}

func (s *StateSerializer) SerializeWorld(world *ecs.World) ([]byte, error) {
	return nil, fmt.Errorf("binary serialization not implemented, use proto snapshots")
}

func (s *StateSerializer) DeserializeWorld(data []byte, world *ecs.World) error {
	return fmt.Errorf("binary deserialization not implemented, use proto snapshots")
}
