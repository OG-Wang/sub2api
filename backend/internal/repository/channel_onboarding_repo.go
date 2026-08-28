package repository

import (
	"context"
	"strings"

	dbaccount "github.com/Wei-Shaw/sub2api/ent/account"
	"github.com/Wei-Shaw/sub2api/ent/channelmonitor"
)

// 一键接入渠道（admin channel onboarding）用到的仓储方法集中在这里。
//
// 这些方法按 Go 的规则挂在 accountRepository / channelMonitorRepository 上，
// 但刻意不写进 account_repo.go 和 channel_monitor_repo.go：那两个都是上游文件，
// 自定义补丁放进去只会扩大每次 rebase 的冲突面。

// ExistsByName 判断是否已有同名账号（软删除行由 SoftDeleteMixin 拦截器自动排除）。
//
// 不挂到 service.AccountRepository 接口上：这是只有管理端接入流程需要的检查，
// 放进接口会逼所有既有测试替身补一个用不到的方法。调用方用类型断言取用。
func (r *accountRepository) ExistsByName(ctx context.Context, name string) (bool, error) {
	return clientFromContext(ctx, r.client).Account.Query().
		Where(dbaccount.NameEQ(strings.TrimSpace(name))).
		Exist(ctx)
}

// ExistsByName 判断是否已有同名渠道监控。
//
// channel_monitors.name 没有唯一索引，所以这只是接入流程边界上的用户友好提示，
// 不是并发安全的约束——真正兜住并发重名的是 groups_name_unique_active。
func (r *channelMonitorRepository) ExistsByName(ctx context.Context, name string) (bool, error) {
	return clientFromContext(ctx, r.client).ChannelMonitor.Query().
		Where(channelmonitor.NameEQ(strings.TrimSpace(name))).
		Exist(ctx)
}
