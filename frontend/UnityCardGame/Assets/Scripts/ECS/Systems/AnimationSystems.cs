using Unity.Burst;
using Unity.Entities;
using Unity.Mathematics;
using Unity.Transforms;
using UnityEngine;

namespace CardGame.ECS.Systems
{
    [BurstCompile]
    public partial struct CardAnimationSystem : ISystem
    {
        public void OnCreate(ref SystemState state)
        {
            state.RequireForUpdate<CardComponent>();
            state.RequireForUpdate<PositionComponent>();
        }

        public void OnUpdate(ref SystemState state)
        {
            float deltaTime = SystemAPI.Time.DeltaTime;

            foreach (var (position, card) in SystemAPI.Query<RefRW<PositionComponent>, RefRO<CardComponent>>())
            {
                position.ValueRW.Value = math.lerp(position.ValueRO.Value, position.ValueRO.Target, deltaTime * 8f);
            }
        }
    }

    [BurstCompile]
    public partial struct CardRotationSystem : ISystem
    {
        public void OnCreate(ref SystemState state)
        {
            state.RequireForUpdate<CardComponent>();
            state.RequireForUpdate<RotationComponent>();
        }

        public void OnUpdate(ref SystemState state)
        {
            float deltaTime = SystemAPI.Time.DeltaTime;

            foreach (var (rotation, card) in SystemAPI.Query<RefRW<RotationComponent>, RefRO<CardComponent>>())
            {
                rotation.ValueRW.Value = math.slerp(rotation.ValueRO.Value, rotation.ValueRO.Target, deltaTime * 8f);
            }
        }
    }

    [BurstCompile]
    public partial struct CardScaleSystem : ISystem
    {
        public void OnCreate(ref SystemState state)
        {
            state.RequireForUpdate<CardComponent>();
            state.RequireForUpdate<ScaleComponent>();
        }

        public void OnUpdate(ref SystemState state)
        {
            float deltaTime = SystemAPI.Time.DeltaTime;

            foreach (var (scale, card) in SystemAPI.Query<RefRW<ScaleComponent>, RefRO<CardComponent>>())
            {
                scale.ValueRW.Value = math.lerp(scale.ValueRO.Value, scale.ValueRO.Target, deltaTime * 8f);
            }
        }
    }

    [BurstCompile]
    public partial struct CardDeathAnimationSystem : ISystem
    {
        public void OnCreate(ref SystemState state)
        {
            state.RequireForUpdate<DyingComponent>();
            state.RequireForUpdate<CardComponent>();
        }

        public void OnUpdate(ref SystemState state)
        {
            float deltaTime = SystemAPI.Time.DeltaTime;
            var ecb = new EntityCommandBuffer(Allocator.TempJob);

            foreach (var (dying, card, position, entity) in SystemAPI.Query<RefRW<DyingComponent>, RefRO<CardComponent>, RefRW<PositionComponent>>().WithEntityAccess())
            {
                dying.ValueRW.Timer += deltaTime;

                position.ValueRW.Value.y += deltaTime * 2f;

                if (dying.ValueRW.Timer > 1.0f)
                {
                    ecb.DestroyEntity(entity);
                }
            }

            ecb.Playback(state.EntityManager);
            ecb.Dispose();
        }
    }

    [BurstCompile]
    public partial struct HoverEffectSystem : ISystem
    {
        public void OnCreate(ref SystemState state)
        {
            state.RequireForUpdate<HoveredComponent>();
            state.RequireForUpdate<CardComponent>();
        }

        public void OnUpdate(ref SystemState state)
        {
            float deltaTime = SystemAPI.Time.DeltaTime;

            foreach (var (hovered, position, card) in SystemAPI.Query<RefRW<HoveredComponent>, RefRW<PositionComponent>, RefRO<CardComponent>>())
            {
                if (hovered.ValueRO.IsHovered)
                {
                    position.ValueRW.Target.y = position.ValueRO.Value.y + 0.5f;
                }
                else
                {
                    position.ValueRW.Target.y = position.ValueRO.Value.y - 0.5f;
                }
            }
        }
    }

    [BurstCompile]
    public partial struct SelectedEffectSystem : ISystem
    {
        public void OnCreate(ref SystemState state)
        {
            state.RequireForUpdate<SelectedComponent>();
            state.RequireForUpdate<CardComponent>();
        }

        public void OnUpdate(ref SystemState state)
        {
            float deltaTime = SystemAPI.Time.DeltaTime;
            float time = (float)SystemAPI.Time.ElapsedTime;

            foreach (var (selected, position, card) in SystemAPI.Query<RefRW<SelectedComponent>, RefRW<PositionComponent>, RefRO<CardComponent>>())
            {
                if (selected.ValueRO.IsSelected)
                {
                    position.ValueRW.Target.y = math.sin(time * 3f) * 0.1f;
                }
            }
        }
    }

    [BurstCompile]
    public partial struct AttackAnimationSystem : ISystem
    {
        public void OnCreate(ref SystemState state)
        {
            state.RequireForUpdate<AttackingComponent>();
            state.RequireForUpdate<CardComponent>();
        }

        public void OnUpdate(ref SystemState state)
        {
            float deltaTime = SystemAPI.Time.DeltaTime;
            var ecb = new EntityCommandBuffer(Allocator.TempJob);

            foreach (var (attacking, position, entity) in SystemAPI.Query<RefRW<AttackingComponent>, RefRW<PositionComponent>>().WithEntityAccess())
            {
                attacking.ValueRW.Timer += deltaTime;

                float progress = attacking.ValueRW.Timer / attacking.ValueRW.Duration;

                if (progress < 0.5f)
                {
                    float t = progress / 0.5f;
                    position.ValueRW.Value = math.lerp(attacking.ValueRO.StartPosition, attacking.ValueRO.TargetPosition, t);
                }
                else
                {
                    float t = (progress - 0.5f) / 0.5f;
                    position.ValueRW.Value = math.lerp(attacking.ValueRO.TargetPosition, attacking.ValueRO.StartPosition, t);
                }

                if (progress >= 1.0f)
                {
                    ecb.RemoveComponent<AttackingComponent>(entity);
                }
            }

            ecb.Playback(state.EntityManager);
            ecb.Dispose();
        }
    }

    [BurstCompile]
    public partial struct SpellCastAnimationSystem : ISystem
    {
        public void OnCreate(ref SystemState state)
        {
            state.RequireForUpdate<CastingComponent>();
            state.RequireForUpdate<SpellComponent>();
        }

        public void OnUpdate(ref SystemState state)
        {
            float deltaTime = SystemAPI.Time.DeltaTime;
            var ecb = new EntityCommandBuffer(Allocator.TempJob);

            foreach (var (casting, position, scale, entity) in SystemAPI.Query<RefRW<CastingComponent>, RefRW<PositionComponent>, RefRW<ScaleComponent>>().WithEntityAccess())
            {
                casting.ValueRW.Timer += deltaTime;

                float progress = casting.ValueRW.Timer / casting.ValueRW.Duration;

                if (progress < 0.5f)
                {
                    float t = progress / 0.5f;
                    scale.ValueRW.Value = 1f + t * 0.5f;
                }
                else
                {
                    float t = (progress - 0.5f) / 0.5f;
                    scale.ValueRW.Value = 1.5f - t * 0.5f;
                    position.ValueRW.Value.y += deltaTime * 2f;
                }

                if (progress >= 1.0f)
                {
                    ecb.DestroyEntity(entity);
                }
            }

            ecb.Playback(state.EntityManager);
            ecb.Dispose();
        }
    }

    public partial struct TransformSyncSystem : ISystem
    {
        public void OnCreate(ref SystemState state)
        {
            state.RequireForUpdate<PositionComponent>();
            state.RequireForUpdate<LocalTransform>();
        }

        public void OnUpdate(ref SystemState state)
        {
            foreach (var (position, transform) in SystemAPI.Query<RefRO<PositionComponent>, RefRW<LocalTransform>>())
            {
                transform.ValueRW.Position = position.ValueRO.Value;
            }
        }
    }
}
