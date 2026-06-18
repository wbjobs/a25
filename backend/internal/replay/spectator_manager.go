package replay

import (
	"context"
	"sync"
	"time"

	pb "github.com/ecscard/game/internal/proto"
)

type SpectatorSession struct {
	SpectatorID   string
	SpectatorName string
	GameID        string
	JoinedAt      int64
	CurrentTimeMs int64
	CurrentFrame  uint64
	PlaybackSpeed float32
	IsPaused      bool
	Stream        chan *pb.SpectatorFrame
	LastAckAt     int64
}

type SpectatorManager struct {
	mu           sync.RWMutex
	store        *ReplayStore
	sessions     map[string]map[string]*SpectatorSession
	sessionCount int64
	gcTicker     *time.Ticker
}

func NewSpectatorManager(store *ReplayStore) *SpectatorManager {
	sm := &SpectatorManager{
		store:    store,
		sessions: make(map[string]map[string]*SpectatorSession),
		gcTicker: time.NewTicker(60 * time.Second),
	}
	go sm.gcLoop()
	return sm
}

func (sm *SpectatorManager) gcLoop() {
	for range sm.gcTicker.C {
		sm.cleanupStaleSessions()
	}
}

func (sm *SpectatorManager) cleanupStaleSessions() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	now := time.Now().UnixNano() / 1e6
	threshold := int64(30 * 60 * 1000)

	for gameID, gameSessions := range sm.sessions {
		for specID, session := range gameSessions {
			if now-session.LastAckAt > threshold {
				close(session.Stream)
				delete(gameSessions, specID)
				sm.sessionCount--
			}
		}
		if len(gameSessions) == 0 {
			delete(sm.sessions, gameID)
		}
	}
}

func (sm *SpectatorManager) JoinSpectator(gameID, specID, specName string, recorder *ReplayRecorder) (*SpectatorSession, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if _, ok := sm.sessions[gameID]; !ok {
		sm.sessions[gameID] = make(map[string]*SpectatorSession)
	}

	startTimeMs := int64(0)
	startFrame := uint64(0)

	elapsedMs := time.Now().UnixNano()/1e6 - recorder.startTime
	delayMs := int64(recorder.GetSpectatorDelayMs())

	if recorder.isLive {
		startTimeMs = elapsedMs - delayMs
		if startTimeMs < 0 {
			startTimeMs = 0
		}
	}

	if startTimeMs > 0 {
		snap, _, _ := recorder.SeekByTime(startTimeMs)
		if snap != nil {
			startFrame = snap.FrameNumber
		}
	}

	session := &SpectatorSession{
		SpectatorID:   specID,
		SpectatorName: specName,
		GameID:        gameID,
		JoinedAt:      time.Now().UnixNano() / 1e6,
		CurrentTimeMs: startTimeMs,
		CurrentFrame:  startFrame,
		PlaybackSpeed: 1.0,
		IsPaused:      false,
		Stream:        make(chan *pb.SpectatorFrame, 256),
		LastAckAt:     time.Now().UnixNano() / 1e6,
	}

	sm.sessions[gameID][specID] = session
	sm.sessionCount++
	recorder.AddSpectator()

	return session, nil
}

func (sm *SpectatorManager) LeaveSpectator(gameID, specID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if gameSessions, ok := sm.sessions[gameID]; ok {
		if session, exists := gameSessions[specID]; exists {
			close(session.Stream)
			delete(gameSessions, specID)
			sm.sessionCount--

			if recorder, ok := sm.store.GetLiveRecorder(gameID); ok {
				recorder.RemoveSpectator()
			}

			if len(gameSessions) == 0 {
				delete(sm.sessions, gameID)
			}
		}
	}
}

func (sm *SpectatorManager) GetSession(gameID, specID string) (*SpectatorSession, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if gameSessions, ok := sm.sessions[gameID]; ok {
		session, exists := gameSessions[specID]
		return session, exists
	}
	return nil, false
}

func (sm *SpectatorManager) SetPlaybackSpeed(gameID, specID string, speed float32) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if gameSessions, ok := sm.sessions[gameID]; ok {
		if session, exists := gameSessions[specID]; exists {
			if speed < 0.25 {
				speed = 0.25
			}
			if speed > 8.0 {
				speed = 8.0
			}
			session.PlaybackSpeed = speed
		}
	}
	return nil
}

func (sm *SpectatorManager) SeekTo(gameID, specID string, targetMs int64, targetFrame uint64, recorder *ReplayRecorder) (*pb.SeekResponse, error) {
	sm.mu.Lock()

	gameSessions, ok := sm.sessions[gameID]
	if !ok {
		sm.mu.Unlock()
		return &pb.SeekResponse{Success: false}, nil
	}

	session, exists := gameSessions[specID]
	if !exists {
		sm.mu.Unlock()
		return &pb.SeekResponse{Success: false}, nil
	}

	sm.mu.Unlock()

	snap, actionIdx, err := recorder.SeekByTime(targetMs)
	if err != nil {
		return &pb.SeekResponse{Success: false}, nil
	}

	sm.mu.Lock()
	session.CurrentTimeMs = targetMs
	session.CurrentFrame = targetFrame
	if snap != nil {
		session.CurrentFrame = snap.FrameNumber
	}
	_ = actionIdx
	sm.mu.Unlock()

	return &pb.SeekResponse{
		Success:       true,
		CurrentTimeMs: targetMs,
		CurrentFrame:  session.CurrentFrame,
		Snapshot:      snap,
	}, nil
}

func (sm *SpectatorManager) StreamLoop(ctx context.Context, gameID, specID string) {
	session, ok := sm.GetSession(gameID, specID)
	if !ok {
		return
	}

	recorder, ok := sm.store.GetLiveRecorder(gameID)
	if !ok {
		return
	}

	actionChan := recorder.Subscribe()
	defer recorder.Unsubscribe(actionChan)

	meta := recorder.GetMeta()
	if meta.InitialSnapshot != nil {
		frame := &pb.SpectatorFrame{
			ServerTimeMs:   time.Now().UnixNano() / 1e6,
			StreamTimeMs:   session.CurrentTimeMs,
			SpectatorCount: recorder.GetSpectatorCount(),
			Phase:          meta.Phase,
			Payload: &pb.SpectatorFrame_FullSnapshot{
				FullSnapshot: meta.InitialSnapshot,
			},
		}
		sm.sendFrame(session, frame)
	}

	_, startIdx, _ := recorder.SeekByTime(session.CurrentTimeMs)
	totalActions := recorder.GetActionsCount()
	segment := recorder.GetSegment(-1, -1, startIdx, totalActions)

	if len(segment.Actions) > 0 {
		sm.playbackActions(ctx, session, recorder, segment.Actions, actionChan)
	}

	if recorder.isLive {
		sm.liveStream(ctx, session, recorder, actionChan)
	}
}

func (sm *SpectatorManager) playbackActions(ctx context.Context, session *SpectatorSession, recorder *ReplayRecorder, actions []*pb.ReplayAction, actionChan chan *pb.ReplayAction) {
	sm.mu.RLock()
	speed := session.PlaybackSpeed
	sm.mu.RUnlock()

	for i := 0; i < len(actions); i++ {
		action := actions[i]

		select {
		case <-ctx.Done():
			return
		default:
		}

		sm.mu.RLock()
		isPaused := session.IsPaused
		sm.mu.RUnlock()

		for isPaused {
			select {
			case <-ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
			sm.mu.RLock()
			isPaused = session.IsPaused
			sm.mu.RUnlock()
		}

		sm.mu.Lock()
		session.CurrentTimeMs = action.RelativeTimeMs
		session.CurrentFrame = action.FrameNumber
		session.LastAckAt = time.Now().UnixNano() / 1e6
		sm.mu.Unlock()

		phase := pb.ReplayPhase_REPLAY_PHASE_LIVE
		if !recorder.isLive {
			phase = pb.ReplayPhase_REPLAY_PHASE_FINISHED
		}

		frame := &pb.SpectatorFrame{
			ServerTimeMs:   time.Now().UnixNano() / 1e6,
			StreamTimeMs:   action.RelativeTimeMs,
			SpectatorCount: recorder.GetSpectatorCount(),
			Phase:          phase,
			Payload: &pb.SpectatorFrame_Action{
				Action: action,
			},
		}
		sm.sendFrame(session, frame)

		if i < len(actions)-1 {
			nextAction := actions[i+1]
			gapMs := nextAction.RelativeTimeMs - action.RelativeTimeMs
			if gapMs > 0 {
				sleepDuration := time.Duration(float64(gapMs)/float64(speed)) * time.Millisecond
				if sleepDuration > 5*time.Second {
					sleepDuration = 5 * time.Second
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(sleepDuration):
				}

				sm.mu.RLock()
				speed = session.PlaybackSpeed
				sm.mu.RUnlock()
			}
		}
	}
}

func (sm *SpectatorManager) liveStream(ctx context.Context, session *SpectatorSession, recorder *ReplayRecorder, actionChan chan *pb.ReplayAction) {
	for {
		select {
		case <-ctx.Done():
			return
		case action, ok := <-actionChan:
			if !ok {
				return
			}

			sm.mu.RLock()
			isPaused := session.IsPaused
			sm.mu.RUnlock()

			delayMs := int64(recorder.GetSpectatorDelayMs())
			elapsed := time.Now().UnixNano()/1e6 - recorder.startTime
			streamAvailable := action.RelativeTimeMs <= (elapsed - delayMs)

			if !streamAvailable {
				waitMs := action.RelativeTimeMs - (elapsed - delayMs)
				if waitMs > 0 {
					select {
					case <-ctx.Done():
						return
					case <-time.After(time.Duration(waitMs) * time.Millisecond):
					}
				}
			}

			for isPaused {
				select {
				case <-ctx.Done():
					return
				case <-time.After(100 * time.Millisecond):
				}
				sm.mu.RLock()
				isPaused = session.IsPaused
				sm.mu.RUnlock()
			}

			sm.mu.Lock()
			session.CurrentTimeMs = action.RelativeTimeMs
			session.CurrentFrame = action.FrameNumber
			session.LastAckAt = time.Now().UnixNano() / 1e6
			sm.mu.Unlock()

			phase := pb.ReplayPhase_REPLAY_PHASE_LIVE
			if !recorder.isLive {
				phase = pb.ReplayPhase_REPLAY_PHASE_FINISHED
			}

			frame := &pb.SpectatorFrame{
				ServerTimeMs:   time.Now().UnixNano() / 1e6,
				StreamTimeMs:   action.RelativeTimeMs,
				SpectatorCount: recorder.GetSpectatorCount(),
				Phase:          phase,
				Payload: &pb.SpectatorFrame_Action{
					Action: action,
				},
			}
			sm.sendFrame(session, frame)
		}
	}
}

func (sm *SpectatorManager) sendFrame(session *SpectatorSession, frame *pb.SpectatorFrame) {
	defer func() {
		recover()
	}()
	select {
	case session.Stream <- frame:
	default:
	}
}

func (sm *SpectatorManager) GetSpectatorCount(gameID string) int32 {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if gameSessions, ok := sm.sessions[gameID]; ok {
		return int32(len(gameSessions))
	}
	return 0
}

func (sm *SpectatorManager) GetTotalSessions() int64 {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.sessionCount
}

func (sm *SpectatorManager) BroadcastChat(gameID, fromName, message string) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	chatMsg := fromName + ": " + message

	if gameSessions, ok := sm.sessions[gameID]; ok {
		for _, session := range gameSessions {
			frame := &pb.SpectatorFrame{
				ServerTimeMs: time.Now().UnixNano() / 1e6,
				StreamTimeMs: session.CurrentTimeMs,
				Payload: &pb.SpectatorFrame_ChatMessage{
					ChatMessage: chatMsg,
				},
			}
			sm.sendFrame(session, frame)
		}
	}
}

func (sm *SpectatorManager) Close() {
	if sm.gcTicker != nil {
		sm.gcTicker.Stop()
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	for gameID, gameSessions := range sm.sessions {
		for specID, session := range gameSessions {
			close(session.Stream)
			delete(gameSessions, specID)
			sm.sessionCount--
		}
		delete(sm.sessions, gameID)
	}
}
