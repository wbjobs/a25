using System;
using System.Collections;
using System.Collections.Generic;
using UnityEngine;

namespace CardGame.Utils
{
    public class UnityMainThreadDispatcher : MonoBehaviour
    {
        private static readonly Queue<Action> _executionQueue = new Queue<Action>();

        public static UnityMainThreadDispatcher Instance { get; private set; }

        private void Awake()
        {
            if (Instance == null)
            {
                Instance = this;
                DontDestroyOnLoad(gameObject);
            }
            else
            {
                Destroy(gameObject);
            }
        }

        private void Update()
        {
            lock (_executionQueue)
            {
                while (_executionQueue.Count > 0)
                {
                    _executionQueue.Dequeue().Invoke();
                }
            }
        }

        public static void Enqueue(Action action)
        {
            if (action == null) throw new ArgumentNullException(nameof(action));

            lock (_executionQueue)
            {
                _executionQueue.Enqueue(action);
            }
        }

        public static void Enqueue(IEnumerator action)
        {
            if (action == null) throw new ArgumentNullException(nameof(action));

            Instance.StartCoroutine(action);
        }

        private void OnDestroy()
        {
            if (Instance == this)
            {
                Instance = null;
            }
        }
    }
}
