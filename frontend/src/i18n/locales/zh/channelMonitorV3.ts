export default {
  channelMonitorV3: {
    title: '分组状态', adminTitle: '分组状态 V3', configure: '配置监控分组', rate: '用户倍率', platformUnknown: '未知平台', rangeAria: '监控时间范围', summaryAria: '分组健康汇总', groupsTitle: '分组健康状态', searchPlaceholder: '搜索分组', allPlatforms: '全部平台', lag: '聚合延迟 {seconds}s', waiting: '等待数据', updatedTo: '数据截至 {time}', loadFailed: '分组状态加载失败', emptyTitle: '暂无分组状态', emptyDescription: '当前时间范围还没有可用的真实请求数据。',
    ranges: { '90m': '90 分钟', '24h': '24 小时', '7d': '7 天', '30d': '30 天' },
    health: { healthy: '健康', warning: '需关注', critical: '异常', unknown: '暂无数据' }, metricsAria: '分组健康指标', metrics: { cacheRate: '缓存率', availability: '可用率', ttft: '首 TOKEN' },
    summary: { groups: '分组数', healthy: '健康', watch: '需关注', critical: '异常' }, timeline: { recent: '最近记录', aria: '分组最近健康记录', waiting: '等待数据', noTraffic: '该时间段暂无用户流量', past: '过去', now: '现在' },
  },
}
