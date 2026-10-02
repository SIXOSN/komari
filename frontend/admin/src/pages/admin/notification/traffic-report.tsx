import { useCallback, useEffect, useMemo, useState } from "react";
import { BarChart2, FileText } from "lucide-react";
import { Button, Callout, Flex, Heading } from "@radix-ui/themes";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { toast } from "sonner";
import ConfigFormTabs, { type ConfigFormItem } from "@/components/admin/ConfigFormTabs";
import Loading from "@/components/loading";
import { useRPC2Call } from "@/contexts/RPC2Context";
import { resolveI18nText } from "@/utils/i18nText";

interface TrafficReportConfiguration {
  enabled: boolean;
  all_nodes: boolean;
  nodes: string[];
  template: string;
  daily_enabled: boolean;
  daily_cron: string;
  weekly_enabled: boolean;
  weekly_cron: string;
  monthly_enabled: boolean;
  monthly_cron: string;
}

export default function TrafficReportPage() {
  const { call } = useRPC2Call();
  const { t, i18n } = useTranslation();
  const navigate = useNavigate();
  const language = i18n.resolvedLanguage || i18n.language;
  const [values, setValues] = useState<Record<string, any>>({});
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    call<any, TrafficReportConfiguration>("admin:getTrafficReportConfiguration")
      .then((cfg) => { if (active) setValues({ ...cfg, nodes: JSON.stringify(cfg.nodes ?? []) }); })
      .catch((err) => { if (active) setError(err instanceof Error ? err.message : String(err)); })
      .finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [call]);

  const resolveText = useCallback((value: Parameters<typeof resolveI18nText>[0]) => resolveI18nText(value, language), [language]);
  const items = useMemo<ConfigFormItem[]>(() => [
    { type: "title", name: t("trafficReport.general", "报告设置") },
    { key: "enabled", type: "switch", name: t("trafficReport.enabled", "启用流量报告"), help: t("trafficReport.enabledHelp", "发送仍受通知总开关控制。") },
    { key: "all_nodes", type: "switch", name: t("trafficReport.allNodes", "为全部节点启用"), help: t("trafficReport.allNodesHelp", "开启后忽略节点选择，始终包含全部节点。") },
    { key: "nodes", type: "nodes", name: t("trafficReport.nodes", "选择节点") },
    { key: "template", type: "richtext", name: t("trafficReport.template", "通知模板"), help: t("trafficReport.templateHelp", "留空使用系统通知模板；支持 {{period}}、{{start}}、{{end}}、{{message}}、{{nodes}}、{{event}}、{{emoji}}、{{time}}。", { interpolation: { skipOnVariables: true } }) },
    { type: "textbox", name: t("trafficReport.retention", "请为 traffic.up 和 traffic.down 保留足够的数据：日报至少 1 天、周报至少 7 天、月报至少 31 天。保存时间过短会导致报告漏数据。") },
    { type: "title", name: t("trafficReport.schedule", "发送计划") },
    { type: "textbox", name: t("trafficReport.scheduleHelp", "Cron 使用 Komari 服务的本地时区，统计上一完整日、周或月；支持五字段、六字段表达式和 @every 1h。") },
    { key: "daily_enabled", type: "switch", name: t("trafficReport.dailyEnabled", "启用日报") },
    { key: "daily_cron", type: "string", name: t("trafficReport.dailyCron", "日报发送时间"), help: t("trafficReport.dailyHelp", "默认 0 9 * * *：每天早上 9 点。") },
    { key: "weekly_enabled", type: "switch", name: t("trafficReport.weeklyEnabled", "启用周报") },
    { key: "weekly_cron", type: "string", name: t("trafficReport.weeklyCron", "周报发送时间"), help: t("trafficReport.weeklyHelp", "默认 0 9 * * 1：每周一早上 9 点。") },
    { key: "monthly_enabled", type: "switch", name: t("trafficReport.monthlyEnabled", "启用月报") },
    { key: "monthly_cron", type: "string", name: t("trafficReport.monthlyCron", "月报发送时间"), help: t("trafficReport.monthlyHelp", "默认 0 9 1 * *：每月 1 号早上 9 点。") },
  ], [t]);

  const save = async () => {
    setSaving(true);
    setError(null);
    try {
      const cfg = { ...values, nodes: JSON.parse(values.nodes || "[]") };
      await call("admin:setTrafficReportConfiguration", cfg);
      toast.success(t("trafficReport.saved", "配置已保存，发送计划已更新。"));
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      setError(message);
      toast.error(message);
    } finally { setSaving(false); }
  };

  if (loading) return <Loading />;
  // Do not allow an unsuccessful initial read to overwrite saved settings.
  if (!Object.keys(values).length) return <Callout.Root color="red"><Callout.Text>{error}</Callout.Text></Callout.Root>;

  return <ConfigFormTabs
    items={items}
    values={values}
    resolveText={resolveText}
    onValueChange={(key, value) => setValues((current) => ({ ...current, [key]: value }))}
    header={<Flex align="center" justify="between" gap="3" wrap="wrap">
      <Flex align="center" gap="2"><BarChart2 size={20} /><Heading size="4">{t("trafficReport.title", "流量报告")}</Heading></Flex>
      <Flex gap="2">
        <Button variant="soft" onClick={() => navigate("/admin/logs/traffic-report")}><FileText size={16} />{t("trafficReport.logs", "流量报告日志")}</Button>
        <Button disabled={saving} onClick={save}>{saving ? t("common.loading") : t("common.save")}</Button>
      </Flex>
    </Flex>}
    notice={error ? <Callout.Root color="red"><Callout.Text>{error}</Callout.Text></Callout.Root> : undefined}
  />;
}
