package httpapi

import (
	"strings"
	"unicode"
)

func localize(message string) string {
	message = strings.TrimSpace(strings.TrimPrefix(message, "validation error: "))
	if translated, ok := chineseMessages[message]; ok {
		return translated
	}
	if strings.HasPrefix(message, "invalid JSON") || strings.Contains(message, "invalid JSON") {
		return "JSON 无效"
	}
	if containsHan(message) {
		return message
	}
	return message
}

func localizedError(code, message string) string {
	message = localize(message)
	if containsHan(message) {
		return message
	}
	if translated, ok := fallbackErrorMessages[code]; ok {
		return translated
	}
	return "请求失败，请稍后再试"
}

func containsHan(message string) bool {
	for _, r := range message {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

var chineseMessages = map[string]string{
	"Complete the initial setup at /setting":                 "请先在 /setting 完成初始设置",
	"A valid CSRF token is required":                         "需要有效的 CSRF 令牌",
	"The application has already been initialized":           "应用已经完成初始化",
	"Unable to create a sign-in session":                     "无法创建登录会话",
	"Username or password is incorrect":                      "用户名或密码不正确",
	"Sign in to continue":                                    "请先登录",
	"Complete the initial setup before serving traffic":      "完成初始设置后才能对外服务",
	"The content store is unavailable":                       "内容存储暂不可用",
	"Site settings not found":                                "未找到站点设置",
	"Unable to read site settings":                           "无法读取站点设置",
	"Unable to read published posts":                         "无法读取已发布文章",
	"Unable to read published terms":                         "无法读取已发布分类或标签",
	"Unable to read published pages":                         "无法读取已发布页面",
	"Unable to read site status":                             "无法读取站点状态",
	"Unable to encode response":                              "无法编码响应",
	"Owner permission is required":                           "需要所有者权限",
	"You do not have permission for this action":             "没有执行此操作的权限",
	"A term with this name or slug already exists":           "已有同名分类或标签",
	"Term not found":                                         "未找到该分类或标签",
	"Post not found":                                         "未找到这篇文章",
	"Page not found":                                         "未找到这个页面",
	"Unable to read post":                                    "无法读取文章",
	"Unable to read neighboring posts":                       "无法读取相邻文章",
	"Unable to read page":                                    "无法读取页面",
	"Unable to read comments":                                "无法读取评论",
	"Unable to read recent comments":                         "无法读取最近评论",
	"Comment not found":                                      "未找到这条评论",
	"Unable to read media":                                   "无法读取媒体",
	"Unable to prepare media storage":                        "无法准备媒体存储",
	"Media files must be 10 MiB or smaller":                  "媒体文件不能超过 10 MiB",
	"A multipart file field is required":                     "请使用 multipart 上传文件",
	"A single multipart file field named file is required":   "请只上传一个名为 file 的文件字段",
	"Unable to open uploaded file":                           "无法打开上传的文件",
	"The uploaded filename is invalid":                       "上传的文件名无效",
	"The uploaded file is empty":                             "上传的文件是空的",
	"Unable to read the uploaded file":                       "无法读取上传的文件",
	"Unable to read uploaded image":                          "无法读取上传的图片",
	"The uploaded file is not a valid image":                 "上传的文件不是有效图片",
	"Image dimensions exceed the supported limit":            "图片尺寸超过支持上限",
	"Only JPEG, PNG, GIF and WebP images are supported":      "只支持 JPEG、PNG、GIF 和 WebP 图片",
	"Unable to create media file":                            "无法创建媒体文件",
	"Unable to write media file":                             "无法写入媒体文件",
	"Unable to finalize media file":                          "无法完成媒体文件",
	"Media not found":                                        "未找到这个媒体文件",
	"Unable to remove media file":                            "无法删除媒体文件",
	"Media storage key is invalid":                           "媒体存储路径无效",
	"Unable to open media":                                   "无法打开媒体文件",
	"A page with this slug already exists":                   "已有使用该固定链接的页面",
	"Unable to read post stats":                              "无法读取文章统计",
	"Unable to read reading settings":                        "无法读取阅读设置",
	"Current password is incorrect":                          "当前密码不正确",
	"Administrator account not found":                        "未找到管理员账户",
	"Unable to update administrator account":                 "无法更新管理员账户",
	"Unable to read users":                                   "无法读取用户",
	"Username is already in use":                             "用户名已被使用",
	"User not found":                                         "未找到该用户",
	"Unable to read audit events":                            "无法读取操作记录",
	"Unable to read administrator account":                   "无法读取管理员账户",
	"Content-Type must be application/json":                  "请求类型必须是 application/json",
	"JSON Content-Type charset must be UTF-8":                "JSON 字符编码必须是 UTF-8",
	"Content-Type must be multipart/form-data":               "请求类型必须是 multipart/form-data",
	"invalid JSON: request body must contain a single value": "JSON 无效",
	"invalid JSON: request body must be an object":           "JSON 请求体必须是对象",
	"The reset token is invalid or expired":                  "重置口令无效或已过期",
	"Password reset email is not configured":                 "站点尚未配置安全邮件投递，暂时无法找回密码",
	"Unable to prepare the backup":                           "无法准备备份",
	"Unable to read revisions":                               "无法读取历史版本",
	"Revision not found":                                     "未找到该历史版本",
	"Unable to restore revision":                             "无法恢复历史版本",
	"Unable to send mail":                                    "无法发送邮件",
	"Unable to read mail":                                    "无法读取站内邮件",
}

var fallbackErrorMessages = map[string]string{
	"already_initialized":       "应用已经完成初始化",
	"backup_busy":               "另一个备份正在处理中，请稍后重试",
	"backup_failed":             "无法准备备份",
	"conflict":                  "请求与现有记录冲突",
	"csrf_failed":               "需要有效的 CSRF 令牌",
	"delete_failed":             "删除失败，请稍后再试",
	"encode_failed":             "无法编码响应",
	"file_too_large":            "上传文件过大",
	"forbidden":                 "没有执行此操作的权限",
	"invalid_credentials":       "用户名或密码不正确",
	"invalid_image":             "上传的文件不是有效图片",
	"invalid_request":           "请求格式无效",
	"image_dimensions_exceeded": "图片尺寸超过支持上限",
	"invalid_token":             "重置口令无效或已过期",
	"mail_unavailable":          "站点尚未配置安全邮件投递，暂时无法找回密码",
	"media_busy":                "另一个媒体上传正在处理中，请稍后重试",
	"method_not_allowed":        "请求方法不受支持",
	"not_found":                 "未找到请求的资源",
	"not_ready":                 "服务尚未就绪",
	"page_taken":                "已有使用该固定链接的页面",
	"post_taken":                "已有使用该固定链接的文章",
	"precondition_required":     "更新内容必须提交 expectedUpdatedAt",
	"password_reset_busy":       "密码找回服务繁忙，请稍后重试",
	"rate_limited":              "请求过于频繁，请稍后再试",
	"read_failed":               "读取失败，请稍后再试",
	"request_too_large":         "请求内容超过允许的大小",
	"reset_failed":              "无法重置密码，请稍后重试",
	"save_failed":               "保存失败，请稍后再试",
	"session_failed":            "无法创建登录会话",
	"session_revoke_failed":     "无法撤销登录会话，请稍后重试",
	"setup_failed":              "初始设置失败",
	"setup_required":            "请先完成初始设置",
	"storage_failed":            "文件存储失败",
	"term_taken":                "已有同名分类或标签",
	"unauthorized":              "请先登录",
	"unsupported_media":         "不支持的媒体格式",
	"username_taken":            "用户名已被使用",
	"validation_error":          "提交的信息不符合要求",
}
