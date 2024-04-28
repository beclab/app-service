package utils

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"time"

	"bytetrade.io/web3os/app-service/pkg/constants"

	"k8s.io/klog/v2"
)

const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// FmtAppName returns application name.
func FmtAppName(name, namespace string) string {
	return fmt.Sprintf("%s-%s", namespace, name)
}

// UserspaceName returns user-space namespace for a user.
func UserspaceName(user string) string {
	return fmt.Sprintf(constants.OwnerNamespaceTempl, constants.OwnerNamespacePrefix, user)
}

// PrettyJSON print pretty json.
func PrettyJSON(v any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		klog.Errorf("Failed to encode json err=%v", err)
	}
	return buf.String()
}

// RandString returns random string with 16 char length.
func RandString() string {
	rand.Seed(time.Now().UnixNano())

	password := make([]byte, 16)
	for i := range password {
		password[i] = charset[rand.Intn(len(charset))]
	}
	return string(password)
}

// Md5String returns md5 for a string.
func Md5String(s string) string {
	hash := md5.Sum([]byte(s))
	hashString := hex.EncodeToString(hash[:])
	return hashString
}
