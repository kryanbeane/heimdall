package mapHelpers

import (
	u "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sync"
)

func RetrieveResourceFromMap(key string, m *sync.Map) (u.Unstructured, bool) {
	if res, ok := m.Load(key); !ok {
		return u.Unstructured{}, false
	} else {
		return res.(u.Unstructured), true
	}
}

func UpdateMapWithResource(resource u.Unstructured, m *sync.Map) {
	m.Store(resource.GetNamespace()+"/"+resource.GetName(), resource)
}

func GetMapLength(m *sync.Map) int {
	var count int
	m.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	return count
}
