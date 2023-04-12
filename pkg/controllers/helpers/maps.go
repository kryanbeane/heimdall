package helpers

import (
	u "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sync"
)

func GetMapLength(m *sync.Map) int {
	var count int
	m.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	return count
}

func RetrieveOrStoreResource(key string, resource u.Unstructured, m *sync.Map) (u.Unstructured, bool) {
	if res, loaded := m.LoadOrStore(key, resource); !loaded {
		return resource, true
	} else if loaded {
		return res.(u.Unstructured), true
	} else {
		return u.Unstructured{}, false
	}
}

func RetrieveResource(key string, m *sync.Map) (u.Unstructured, bool) {
	if res, ok := m.Load(key); ok {
		return res.(u.Unstructured), true
	} else {
		return u.Unstructured{}, true
	}
}

func UpdateMapWithResource(resource u.Unstructured, key string, m *sync.Map) {
	m.Store(key, resource)
}

func DeleteResource(key string, m *sync.Map) {
	m.Delete(key)
}
