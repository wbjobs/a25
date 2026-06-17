using Unity.Entities;
using Unity.Mathematics;
using Unity.Transforms;
using UnityEngine;

namespace CardGame.ECS
{
    public class GameObjectConverter
    {
        private readonly EntityManager _entityManager;

        public GameObjectConverter(EntityManager entityManager)
        {
            _entityManager = entityManager;
        }

        public Entity CreateCardPrefab()
        {
            var entity = _entityManager.CreateEntity();

            _entityManager.AddComponentData(entity, new CardComponent());
            _entityManager.AddComponentData(entity, new OwnerComponent());
            _entityManager.AddComponentData(entity, new HealthComponent());
            _entityManager.AddComponentData(entity, new AttackComponent());
            _entityManager.AddComponentData(entity, new PositionComponent
            {
                Value = float3.zero,
                Target = float3.zero
            });
            _entityManager.AddComponentData(entity, LocalTransform.Identity);
            _entityManager.AddComponentData(entity, new Scale { Value = 1f });

            _entityManager.SetName(entity, "CardPrefab");

            return entity;
        }

        public Entity CreatePlayerPrefab()
        {
            var entity = _entityManager.CreateEntity();

            _entityManager.AddComponentData(entity, new PlayerComponent());
            _entityManager.AddComponentData(entity, new OwnerComponent());
            _entityManager.AddComponentData(entity, new HealthComponent());
            _entityManager.AddComponentData(entity, new AttackComponent());
            _entityManager.AddComponentData(entity, new PositionComponent
            {
                Value = float3.zero,
                Target = float3.zero
            });
            _entityManager.AddComponentData(entity, LocalTransform.Identity);
            _entityManager.AddComponentData(entity, new Scale { Value = 1f });

            _entityManager.SetName(entity, "PlayerPrefab");

            return entity;
        }

        public Entity ConvertGameObject(GameObject gameObject)
        {
            var entity = _entityManager.CreateEntity();

            var transform = gameObject.transform;
            _entityManager.AddComponentData(entity, LocalTransform.FromPositionRotationScale(
                transform.position,
                transform.rotation,
                transform.lossyScale.x
            ));

            var meshFilter = gameObject.GetComponent<MeshFilter>();
            if (meshFilter != null)
            {
                _entityManager.AddComponentData(entity, new RenderBounds
                {
                    Value = meshFilter.sharedMesh.bounds.ToAABB()
                });
            }

            var collider = gameObject.GetComponent<Collider>();
            if (collider != null)
            {
                var boxCollider = collider as BoxCollider;
                if (boxCollider != null)
                {
                    _entityManager.AddComponentData(entity, new PhysicsCollider
                    {
                        Value = Unity.Physics.BoxCollider.Create(new Unity.Physics.BoxGeometry
                        {
                            Center = boxCollider.center,
                            Size = boxCollider.size
                        })
                    });
                }
            }

            return entity;
        }

        public void BindEntityToGameObject(Entity entity, GameObject gameObject)
        {
            var binding = gameObject.AddComponent<EntityBinding>();
            binding.Entity = entity;
        }
    }

    public class EntityBinding : MonoBehaviour
    {
        public Entity Entity;
        public EntityManager EntityManager;

        private void LateUpdate()
        {
            if (EntityManager != null && EntityManager.Exists(Entity))
            {
                if (EntityManager.HasComponent<LocalTransform>(Entity))
                {
                    var transform = EntityManager.GetComponentData<LocalTransform>(Entity);
                    this.transform.position = transform.Position;
                    this.transform.rotation = transform.Rotation;
                    this.transform.localScale = Vector3.one * transform.Scale;
                }
            }
        }
    }
}
