package core

import (
	"fmt"
	"hash/fnv"
	"testing"
)

func bindingMethodID(methodName string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(fmt.Sprintf("app/core.ConfigManager.%s", methodName)))
	return h.Sum32()
}

func TestConfigManagerBindingMethodIDs(t *testing.T) {
	expectedIDs := map[string]uint32{
		"GetUploadHistory":        1608362970,
		"GetUploadSessionDetails": 3658049140,
		"ClearUploadHistory":      851591065,
		"ExportUploadSessionCSV":  4074383194,
		"GetAutoBackup":           926327962,
		"SetAutoBackupEnabled":    2074538803,
		"SetAutoBackupInterval":   1645990227,
		"AddAutoBackupFolder":     402250737,
		"RemoveAutoBackupFolder":  3259039084,
	}

	for method, expectedID := range expectedIDs {
		actualID := bindingMethodID(method)
		if actualID != expectedID {
			t.Errorf("Method %s expected ID %d, got %d", method, expectedID, actualID)
		}
	}
}
