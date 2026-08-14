package protocol

import (
	"encoding/json"
	"strconv"
)

// StringInt64s is a []int64 that serializes to JSON as an array of quoted strings,
// e.g. ["123","456"] instead of [123,456].
// This prevents JavaScript consumers from losing precision on snowflake IDs.
type StringInt64s []int64

func (s StringInt64s) MarshalJSON() ([]byte, error) {
	strs := make([]string, len(s))
	for i, v := range s {
		strs[i] = strconv.FormatInt(v, 10)
	}
	return json.Marshal(strs)
}

func (s *StringInt64s) UnmarshalJSON(data []byte) error {
	var strs []string
	if err := json.Unmarshal(data, &strs); err == nil {
		result := make([]int64, len(strs))
		for i, str := range strs {
			v, err := strconv.ParseInt(str, 10, 64)
			if err != nil {
				return err
			}
			result[i] = v
		}
		*s = result
		return nil
	}
	var nums []int64
	if err := json.Unmarshal(data, &nums); err != nil {
		return err
	}
	*s = nums
	return nil
}
