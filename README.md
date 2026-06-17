# ECS Card Battle Game

基于ECS架构的实时多人在线卡牌对战游戏

## 架构概览

```
┌─────────────────┐     gRPC Stream      ┌─────────────────┐
│   Unity DOTS    │◄────────────────────►│   Envoy Gateway │
│   Frontend      │                      └────────┬────────┘
└─────────────────┘                               │
                                                   │ gRPC
                                                   ▼
┌─────────────┐  Pub/Sub  ┌────────────────┐  RPC  ┌──────────────┐
│   Redis     │◄────────►│  Game Server   │◄────►│  Match Server│
│  (State)    │          │  (ECS Engine)  │       │              │
└─────────────┘          └────────┬───────┘       └──────┬───────┘
                                   │                      │
                                   │                      │
                                   ▼                      ▼
                           ┌────────────────┐       ┌──────────────┐
                           │   MongoDB      │       │   MongoDB    │
                           │ (Card Library) │       │  (Match/Stats)│
                           └────────────────┘       └──────────────┘
```

## 技术栈

- **后端**: Go 1.21+
- **网关**: Envoy
- **状态同步**: Redis 7.0+
- **数据存储**: MongoDB 6.0+
- **通信**: gRPC + Protobuf (流式传输)
- **前端**: Unity 2022.3+ with DOTS/ECS
- **架构**: ECS (Entity-Component-System)

## 游戏规则

- 每方玩家: 30张卡牌, 20点生命值
- 卡牌类型:
  - **随从**: 攻击力/生命值, 站场作战
  - **法术**: 直伤/抽牌等即时效果
  - **武器**: 给英雄附加攻击力, 持续回合
- 回合制: 每回合自动抽1张牌, 法力值递增
- 胜利条件: 对方生命值降为0

## 快速开始

```bash
# 启动所有服务
docker-compose up -d

# 单独启动游戏服务器
cd backend
go run cmd/game-server/main.go
```
