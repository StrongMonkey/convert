package convert

import (
	"encoding/json"
	"fmt"
	v3 "github.com/rancher/go-rancher/v3"
	"github.com/docker/go-units"
)

func ToMapString(m map[string]interface{}) map[string]string {
	r := map[string]string{}
	for k, v := range m {
		r[k] = InterfaceToString(v)
	}
	return r
}

func InterfaceToString(v interface{}) string {
	value, ok := v.(string)
	if ok {
		return value
	}
	return ""
}

func ConvertUlimits(ulimits []v3.Ulimit) []*units.Ulimit {
	r := []*units.Ulimit{}
	for _, ulimit := range ulimits {
		r = append(r, &units.Ulimit{
			Hard: ulimit.Hard,
			Soft: ulimit.Soft,
			Name: ulimit.Name,
		})
	}
	return r
}

func Unmarshalling(data interface{}, v interface{}) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshall object. Body: %v", data)
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return fmt.Errorf("failed to unmarshall object. Body: %v", string(raw))
	}
	return nil
}