package api

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func String(o Object, keys ...string) string {
	for _, key := range keys {
		if v, ok := o[key]; ok && v != nil {
			switch x := v.(type) {
			case string:
				if strings.TrimSpace(x) != "" {
					return x
				}
			case float64:
				return strconv.FormatFloat(x, 'f', -1, 64)
			case bool:
				return strconv.FormatBool(x)
			default:
				return fmt.Sprint(x)
			}
		}
	}
	return ""
}

func Number(o Object, keys ...string) float64 {
	for _, key := range keys {
		v, ok := o[key]
		if !ok || v == nil {
			continue
		}
		switch x := v.(type) {
		case float64:
			return x
		case float32:
			return float64(x)
		case int:
			return float64(x)
		case int64:
			return float64(x)
		case jsonNumber:
			f, _ := strconv.ParseFloat(string(x), 64)
			return f
		case string:
			f, _ := strconv.ParseFloat(x, 64)
			return f
		}
	}
	return 0
}

type jsonNumber string

func Time(o Object, keys ...string) (time.Time, bool) {
	for _, key := range keys {
		v := String(o, key)
		if v == "" {
			continue
		}
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02"} {
			if t, err := time.Parse(layout, v); err == nil {
				return t, true
			}
		}
	}
	return time.Time{}, false
}

func NestedString(o Object, outer string, keys ...string) string {
	v, ok := o[outer]
	if !ok || v == nil {
		return ""
	}
	m, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	return String(Object(m), keys...)
}

func StringSlice(o Object, key string) []string {
	v, ok := o[key]
	if !ok || v == nil {
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		switch x := item.(type) {
		case string:
			if x != "" {
				out = append(out, x)
			}
		case map[string]any:
			name := String(Object(x), "display_name", "name", "email", "id")
			if name != "" {
				out = append(out, name)
			}
		}
	}
	return out
}
