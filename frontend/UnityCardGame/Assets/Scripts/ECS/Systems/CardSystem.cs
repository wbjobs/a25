using Unity.Burst;
using Unity.Collections;
using Unity.Entities;
using Unity.Mathematics;
using Unity.Transforms;

namespace CardGame.ECS
{
    [BurstCompile]
    public partial struct CardPositionSystem : ISystem
    {
        public void OnUpdate(ref SystemState state)
        {
            foreach (var (position, card) in SystemAPI.Query<RefRW<PositionComponent>, RefRO<CardComponent>>())
            {
                float3 pos = position.ValueRO.Value;
                float3 target = position.ValueRO.Target;
                pos = math.lerp(pos, target, 0.1f);
                position.ValueRW.Value = pos;
            }
        }
    }

    [BurstCompile]
    public partial struct CardAnimationSystem : ISystem
    {
        public void OnUpdate(ref SystemState state)
        {
            float deltaTime = SystemAPI.Time.DeltaTime;

            foreach (var (animation, transform) in SystemAPI.Query<RefRW<AnimationComponent>, RefRW<LocalTransform>>())
            {
                if (!animation.ValueRO.IsPlaying) continue;

                animation.ValueRW.AnimationTime += deltaTime;
                float t = animation.ValueRW.AnimationTime / animation.ValueRW.AnimationDuration;

                if (t >= 1.0f)
                {
                    animation.ValueRW.IsPlaying = false;
                    animation.ValueRW.AnimationTime = 0;
                    continue;
                }

                switch (animation.ValueRO.AnimationType)
                {
                    case 0:
                        float3 scale = transform.ValueRO.Scale;
                        scale.y = math.sin(t * math.PI) * 0.2f + 1.0f;
                        transform.ValueRW.Scale = scale;
                        break;
                    case 1:
                        float3 pos = transform.ValueRO.Position;
                        pos.y += math.sin(t * math.PI * 2) * 0.1f;
                        transform.ValueRW.Position = pos;
                        break;
                    case 2:
                        quaternion rot = transform.ValueRO.Rotation;
                        rot = quaternion.AxisAngle(new float3(0, 1, 0), t * math.PI * 2);
                        transform.ValueRW.Rotation = rot;
                        break;
                }
            }
        }
    }

    [BurstCompile]
    public partial struct CardDeathSystem : ISystem
    {
        public void OnUpdate(ref SystemState state)
        {
            var ecb = new EntityCommandBuffer(Allocator.TempJob);
            var parallelEcb = ecb.AsParallelWriter();

            foreach (var (card, entity) in SystemAPI.Query<RefRO<CardComponent>>().WithAll<DyingComponent>().WithEntityAccess())
            {
                parallelEcb.DestroyEntity(entity.Index, entity);
            }

            ecb.Playback(state.EntityManager);
            ecb.Dispose();
        }
    }

    [BurstCompile]
    public partial struct HoverEffectSystem : ISystem
    {
        public void OnUpdate(ref SystemState state)
        {
            foreach (var (transform, hover, card) in SystemAPI.Query<RefRW<LocalTransform>, EnabledRefRO<HoverComponent>, RefRO<CardComponent>>())
            {
                if (hover.ValueRO)
                {
                    float3 pos = transform.ValueRO.Position;
                    pos.y += 0.5f;
                    transform.ValueRW.Position = pos;
                }
            }
        }
    }

    [BurstCompile]
    public partial struct SelectedEffectSystem : ISystem
    {
        public void OnUpdate(ref SystemState state)
        {
            foreach (var (transform, selected, card) in SystemAPI.Query<RefRW<LocalTransform>, EnabledRefRO<SelectedComponent>, RefRO<CardComponent>>())
            {
                if (selected.ValueRO)
                {
                    float3 scale = transform.ValueRO.Scale;
                    scale *= 1.1f;
                    transform.ValueRW.Scale = scale;
                }
            }
        }
    }
}
