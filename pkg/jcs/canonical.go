package jcs

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode/utf8"
)

func Canonicalize(v any) ([]byte, error) {
	return canonicalizeValue(v)
}

func canonicalizeValue(v any) ([]byte, error) {
	switch val := v.(type) {
	case nil:
		return []byte("null"), nil
	case bool:
		if val {
			return []byte("true"), nil
		}
		return []byte("false"), nil
	case float64:
		return canonicalizeNumber(val), nil
	case int:
		return canonicalizeNumber(float64(val)), nil
	case int64:
		return canonicalizeNumber(float64(val)), nil
	case string:
		strBytes, err := canonicalizeString(val)
		if err != nil {
			return nil, err
		}
		return strBytes, nil
	case []any:
		return canonicalizeArray(val)
	case []map[string]any:
		return canonicalizeArrayOfMaps(val)
	case map[string]any:
		return canonicalizeObject(val)
	default:
		return nil, fmt.Errorf("unsupported type: %T", v)
	}
}

func canonicalizeNumber(n float64) []byte {
	if n != n {
		return []byte("null")
	}

	if n == 0 {
		// 检测负零
		if math.Float64bits(n)&(1<<63) != 0 {
			return []byte("-0")
		}
		return []byte("0")
	}

	if math.IsInf(n, 0) {
		return []byte("null")
	}

	intPart := int64(n)
	if float64(intPart) == n {
		return []byte(fmt.Sprintf("%d", intPart))
	}

	prec := 15
	result := fmt.Sprintf("%.*f", prec, n)
	result = strings.TrimRight(result, "0")
	result = strings.TrimRight(result, ".")

	return []byte(result)
}

func canonicalizeString(s string) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('"')

	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])

		switch r {
		case '"':
			buf.WriteString(`\"`)
		case '\\':
			buf.WriteString(`\\`)
		case '\n':
			buf.WriteString(`\n`)
		case '\r':
			buf.WriteString(`\r`)
		case '\t':
			buf.WriteString(`\t`)
		case '\b':
			buf.WriteString(`\b`)
		case '\f':
			buf.WriteString(`\u000c`)
		default:
			if r < 0x20 {
				buf.WriteString(fmt.Sprintf(`\u%04x`, r))
			} else {
				buf.WriteRune(r)
			}
		}
		i += size
	}

	buf.WriteByte('"')
	return buf.Bytes(), nil
}

func canonicalizeArray(arr []any) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('[')

	for i, v := range arr {
		if i > 0 {
			buf.WriteByte(',')
		}
		cv, err := canonicalizeValue(v)
		if err != nil {
			return nil, err
		}
		buf.Write(cv)
	}

	buf.WriteByte(']')
	return buf.Bytes(), nil
}

func canonicalizeArrayOfMaps(arr []map[string]any) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('[')

	for i, m := range arr {
		if i > 0 {
			buf.WriteByte(',')
		}
		cv, err := canonicalizeObject(m)
		if err != nil {
			return nil, err
		}
		buf.Write(cv)
	}

	buf.WriteByte(']')
	return buf.Bytes(), nil
}

func canonicalizeObject(obj map[string]any) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')

	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for i, k := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}

		ck, err := canonicalizeString(k)
		if err != nil {
			return nil, err
		}
		buf.Write(ck)
		buf.WriteByte(':')

		cv, err := canonicalizeValue(obj[k])
		if err != nil {
			return nil, err
		}
		buf.Write(cv)
	}

	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func CanonicalizeToString(v any) (string, error) {
	data, err := Canonicalize(v)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func MarshalCanonical(v any) ([]byte, error) {
	return Canonicalize(v)
}

func MustCanonicalize(v any) []byte {
	data, err := Canonicalize(v)
	if err != nil {
		panic(err)
	}
	return data
}

func StripNonSignatureFields(obj map[string]any) map[string]any {
	result := make(map[string]any)

	for k, v := range obj {
		if k == "internal_metrics" {
			continue
		}
		if strings.HasPrefix(k, "_") {
			continue
		}

		if childMap, ok := v.(map[string]any); ok {
			if k == "extensions" {
				strippedExt := make(map[string]any)
				for extK, extV := range childMap {
					if !strings.HasPrefix(extK, "_") {
						if strippedChild := stripExtensionField(extV); strippedChild != nil {
							strippedExt[extK] = strippedChild
						}
					}
				}
				if len(strippedExt) > 0 {
					result[k] = strippedExt
				}
			} else {
				result[k] = StripNonSignatureFields(childMap)
			}
		} else if childArr, ok := v.([]any); ok {
			result[k] = stripArrayNonSignatureFields(childArr)
		} else {
			result[k] = v
		}
	}

	return result
}

func stripExtensionField(v any) any {
	if childMap, ok := v.(map[string]any); ok {
		stripped := make(map[string]any)
		for k, val := range childMap {
			if !strings.HasPrefix(k, "_") {
				stripped[k] = val
			}
		}
		return stripped
	}
	return v
}

func stripArrayNonSignatureFields(arr []any) []any {
	result := make([]any, 0, len(arr))
	for _, v := range arr {
		if childMap, ok := v.(map[string]any); ok {
			result = append(result, StripNonSignatureFields(childMap))
		} else {
			result = append(result, v)
		}
	}
	return result
}

func ComputeSHA256Hex(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func CanonicalizeAndHash(v any) (string, error) {
	canonical, err := Canonicalize(v)
	if err != nil {
		return "", fmt.Errorf("canonicalization failed: %w", err)
	}
	return ComputeSHA256Hex(canonical), nil
}

func SanitizeResponse(s string) string {
	var buf bytes.Buffer
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			buf.WriteString("\uFFFD")
		} else if r < 0x20 && r != '\n' && r != '\r' && r != '\t' {
			buf.WriteString(fmt.Sprintf(`\u%04x`, r))
		} else {
			buf.WriteRune(r)
		}
		i += size
	}
	return buf.String()
}
