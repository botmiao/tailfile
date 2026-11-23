package gui

import (
	"errors"
	"strings"
	"unicode/utf8"

	"tailfile.local/internal/agent"
)

var windowsReservedNames = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true,
	"COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true,
	"LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// ValidateName checks a new file or folder name against cross-platform rules.
func ValidateName(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", errors.New("名称不能为空")
	}
	name := strings.TrimLeft(raw, " \t")
	if name == "." || name == ".." {
		return "", errors.New("名称不能为 . 或 ..")
	}
	if len(name) > 255 {
		return "", errors.New("名称过长")
	}
	if strings.ContainsAny(name, "/\\") {
		return "", errors.New("名称不能包含路径分隔符")
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return "", errors.New("名称不能包含控制字符")
		}
	}
	if strings.ContainsAny(name, `<>:"|?*`) {
		return "", errors.New(`名称不能包含 < > : " | ? *`)
	}
	if strings.HasSuffix(name, " ") || strings.HasSuffix(name, ".") {
		return "", errors.New("名称不能以空格或句点结尾")
	}
	stem, _, _ := strings.Cut(name, ".")
	if windowsReservedNames[strings.ToUpper(stem)] {
		return "", errors.New("该名称是系统保留名称")
	}
	return name, nil
}

// RenameStemRuneLen returns the rune length of the filename stem to preselect during in-place rename.
func RenameStemRuneLen(name string, isDir bool) int {
	if isDir {
		return utf8.RuneCountInString(name)
	}
	dot := strings.LastIndexByte(name, '.')
	if dot <= 0 {
		return utf8.RuneCountInString(name)
	}
	return utf8.RuneCountInString(name[:dot])
}

// UniqueFolderName generates "新建文件夹", "新建文件夹 2", ... avoiding collisions.
func UniqueFolderName(entries []agent.Entry) string {
	used := make(map[string]bool, len(entries))
	for _, e := range entries {
		used[strings.ToLower(e.Name)] = true
	}
	base := "新建文件夹"
	if !used[strings.ToLower(base)] {
		return base
	}
	for i := 2; ; i++ {
		cand := base + " " + itoa(i)
		if !used[strings.ToLower(cand)] {
			return cand
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
