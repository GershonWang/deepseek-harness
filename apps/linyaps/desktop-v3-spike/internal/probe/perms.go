// 本文件是权限策略的验证开关：从宿主与容器共享的 /tmp 读取覆盖值。
//
// 为什么需要它：v3 的权限策略是编译期写进窗口选项的，而「拒绝」路径必须与「放行」
// 路径用同一个二进制对比才有说服力（重新构建一次只能验一种）。ll-builder run 不会
// 把调用者的环境变量带进容器，但宿主 /tmp 与容器 /tmp 是同一个目录，因此用文件作
// 传递通道：宿主写入 /tmp/dsh-spike-perms，容器内的进程读取它。
//
// 这是验证用的后门，不属于产品代码：正式客户端会把这些取值写死在窗口选项里。
package probe

import (
	"os"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// permsOverridePath 是策略覆盖文件路径。宿主与容器共享 /tmp，因此两边看到的是同一个文件。
const permsOverridePath = "/tmp/dsh-spike-perms"

// PermissionPolicy 返回本次运行生效的权限策略。
//
// 默认值与客户端现有策略对齐：麦克风放行，摄像头拒绝（屏幕共享请求带 video 标志，
// 因此一并被拒）。文件里出现 microphone=deny 等行时覆盖默认值。
func PermissionPolicy() map[application.PermissionType]application.Permission {
	policy := map[application.PermissionType]application.Permission{
		application.PermissionMicrophone: application.PermissionAllow,
		application.PermissionCamera:     application.PermissionDeny,
	}

	data, err := os.ReadFile(permsOverridePath)
	if err != nil {
		return policy
	}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		permission, known := parsePermission(value)
		if !known {
			continue
		}
		switch strings.TrimSpace(key) {
		case "microphone":
			policy[application.PermissionMicrophone] = permission
		case "camera":
			policy[application.PermissionCamera] = permission
		}
	}
	return policy
}

// parsePermission 把配置值映射成 v3 的权限取值。
func parsePermission(value string) (application.Permission, bool) {
	switch strings.TrimSpace(value) {
	case "allow":
		return application.PermissionAllow, true
	case "deny":
		return application.PermissionDeny, true
	case "default":
		return application.PermissionDefault, true
	default:
		return 0, false
	}
}

// PermissionPolicyNames 返回策略的可读描述，写进报告便于对照运行结果。
func PermissionPolicyNames(policy map[application.PermissionType]application.Permission) map[string]string {
	name := func(p application.Permission) string {
		switch p {
		case application.PermissionAllow:
			return "allow"
		case application.PermissionDeny:
			return "deny"
		default:
			return "default"
		}
	}
	return map[string]string{
		"microphone": name(policy[application.PermissionMicrophone]),
		"camera":     name(policy[application.PermissionCamera]),
	}
}
