import type { RouteResult } from '@/utils/rpc'
import { formatDateTime } from '@/utils/helper'

export interface RouteBadge {
  family: string
  label: string
  tooltip: string
  warning: boolean
}

const ROUTE_NAMES: Record<string, string> = {
  CTGGIA: '中国电信 CTGNet / CN2 GIA（依据可见跳点推断）',
  CN2GIA: '中国电信 CN2 GIA（依据可见跳点推断，不能据此确认商业服务等级）',
  CN2GT: '中国电信 CN2 GT（依据可见跳点推断）',
  CN2: '中国电信 CN2 骨干网（无法单独确认 GIA 服务等级）',
  CTGNet: '中国电信 CTGNet（无法单独确认 GIA 服务等级）',
  '163': '中国电信 ChinaNet 163 骨干网',
  '9929': '中国联通 AS9929 精品网',
  '10099': '中国联通 AS10099 国际网',
  '4837': '中国联通 AS4837 骨干网',
  '4808': '中国联通 AS4808 骨干网',
  CMIN2: '中国移动 CMIN2 精品网',
  CMI: '中国移动 CMI 国际网',
  CMNET: '中国移动 CMNET 骨干网',
  CERNET: '中国教育和科研计算机网 CERNET',
  CSTNET: '中国科技网 CSTNET',
  NO_IPV6: '测速点没有 IPv6（AAAA）地址，无法检测',
}

export function getRouteBadges(results: RouteResult[], uuid: string, taskId: number, taskType?: string): RouteBadge[] {
  if (taskType !== 'tcp')
    return []

  const routes = results
    .filter(result => result.uuid === uuid && result.task_id === taskId)
    .sort((left, right) => (left.family || 'ipv4').localeCompare(right.family || 'ipv4'))

  if (!routes.length)
    return [{ family: '', label: '待检测', tooltip: '等待 Agent 探测回国路由', warning: false }]

  return routes.map((result) => {
    const label = result.label === 'NO_IPV6' ? '无IPv6' : result.label || '未知'
    return {
      family: result.family || 'ipv4',
      label,
      tooltip: `${ROUTE_NAMES[result.label] || result.label || '线路无法判断'}\n回国路由检测时间：${formatDateTime(result.checked_at)}`,
      warning: label === '未知' || label === '无IPv6',
    }
  })
}
