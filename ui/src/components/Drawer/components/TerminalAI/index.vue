<script setup lang="ts">
import { marked } from 'marked';
import DOMPurify from 'dompurify';
import { computed, nextTick, onMounted, onUnmounted, ref } from 'vue';
import {
  Bot,
  ChevronDown,
  ChevronRight,
  CircleAlert,
  CircleCheck,
  CircleDot,
  LoaderCircle,
  Send,
  Square,
  SquareTerminal,
  UserRound,
} from 'lucide-vue-next';

import mittBus from '@/utils/mittBus';
import { useColor } from '@/hooks/useColor';
import { useConnectionStore } from '@/store/modules/useConnection';
import { buildJSONEnvelope, ENVELOPE_CHAT } from '@/websocket/envelope';

type EventData = Record<string, any>;

interface ChatPart {
  type: string;
  text?: string;
  data?: EventData;
}

interface ChatMessage {
  id: string;
  role: 'user' | 'assistant';
  metadata?: EventData;
  parts: ChatPart[];
}

interface ViewStep {
  id: string;
  key: string;
  index: number;
  title: string;
  objective: string;
  status: string;
  command?: EventData;
  execution?: EventData;
  acl?: EventData;
}

interface TextItem {
  kind: 'text';
  key: string;
  role: ChatMessage['role'];
  text: string;
}

interface PlanItem {
  kind: 'plan';
  key: string;
  id: string;
  summary: string;
  steps: ViewStep[];
}

interface AlertItem {
  kind: 'alert';
  key: string;
  data: EventData;
}

type ViewItem = TextItem | PlanItem | AlertItem;

const connectionStore = useConnectionStore();
const { alpha, currentMainColor, darken, lighten } = useColor();
const messages = ref<ChatMessage[]>([]);
const messagesElement = ref<HTMLElement>();
const input = ref('');
const runtimeStatus = ref('');
const busy = ref(false);
const approvalThreshold = ref(2);
const executionMode = ref('auto');
const decisions = ref(new Set<string>());
const executionOverrides = ref(new Map<string, string>());
const expansionOverrides = ref(new Map<string, boolean>());
const errorText = ref('');

const thresholdOptions = [
  { label: '全部命令审批', value: 1 },
  { label: '风险 2 及以上审批', value: 2 },
  { label: '风险 3 及以上审批', value: 3 },
  { label: '仅风险 4 审批', value: 4 },
];

const modeOptions = computed(() => [
  { label: 'AI 自动选择', value: 'auto' },
  { label: '仅当前 PTY', value: 'pty_only' },
  {
    label: '仅后台执行',
    value: 'background_only',
    disabled: !connectionStore.terminalAIBackground,
  },
]);

const themeStyle = computed(() => {
  const baseColor = currentMainColor.value;
  const accent = lighten(34, baseColor);
  const border = lighten(22, baseColor);

  return {
    '--ai-accent': accent,
    '--ai-accent-soft': alpha(0.16, accent),
    '--ai-bg': darken(8, baseColor),
    '--ai-border': alpha(0.28, border),
    '--ai-hover': lighten(5, baseColor),
    '--ai-surface': darken(3, baseColor),
    '--ai-surface-raised': lighten(1, baseColor),
  };
});

const viewItems = computed<ViewItem[]>(() => {
  const items: ViewItem[] = [];
  const plans = new Map<string, PlanItem>();
  const steps = new Map<string, ViewStep>();

  const ensurePlan = (id: string, key: string) => {
    let plan = plans.get(id);
    if (!plan) {
      plan = {
        kind: 'plan',
        key,
        id,
        summary: '执行计划',
        steps: [],
      };
      plans.set(id, plan);
      items.push(plan);
    }
    return plan;
  };

  const ensureStep = (plan: PlanItem, data: EventData) => {
    const id = String(data.stepId || data.id || `step-${plan.steps.length + 1}`);
    const key = `${plan.id}:${id}`;
    let step = steps.get(key);
    if (!step) {
      step = {
        id,
        key,
        index: Number(data.step) || plan.steps.length + 1,
        title: String(data.title || `步骤 ${plan.steps.length + 1}`),
        objective: String(data.objective || ''),
        status: String(data.status || 'pending'),
      };
      steps.set(key, step);
      plan.steps.push(step);
    }
    return step;
  };

  messages.value.forEach((message) => {
    message.parts.forEach((part, partIndex) => {
      if (part.type === 'text') {
        items.push({
          kind: 'text',
          key: `${message.id}-text-${partIndex}`,
          role: message.role,
          text: part.text || '',
        });
        return;
      }

      const data = part.data || {};
      if (part.type === 'data-plan') {
        const planId = String(data.id || `plan-${message.id}`);
        const plan = ensurePlan(planId, `${message.id}-plan-${partIndex}`);
        plan.summary = String(data.summary || plan.summary);
        const rawSteps = Array.isArray(data.steps) ? data.steps : [];
        rawSteps.forEach((rawStep: EventData, index: number) => {
          const step = ensureStep(plan, rawStep);
          step.index = index + 1;
          step.title = String(rawStep.title || step.title);
          step.objective = String(rawStep.objective || step.objective);
          step.status = String(rawStep.status || step.status);
        });
        return;
      }

      if (part.type === 'data-command' || part.type === 'data-approval') {
        const planId = String(data.planId || `plan-${message.id}`);
        const plan = ensurePlan(planId, `${message.id}-plan-${partIndex}`);
        const step = ensureStep(plan, data);
        step.command = {
          ...step.command,
          ...data,
          partType: part.type,
        };
        return;
      }

      if (part.type === 'data-execution') {
        const planId = String(data.planId || `plan-${message.id}`);
        const plan = ensurePlan(planId, `${message.id}-plan-${partIndex}`);
        const step = ensureStep(plan, data);
        step.execution = { ...step.execution, ...data };
        return;
      }

      if (part.type === 'data-command-acl') {
        if (data.planId || data.stepId) {
          const planId = String(data.planId || `plan-${message.id}`);
          const plan = ensurePlan(planId, `${message.id}-plan-${partIndex}`);
          ensureStep(plan, data).acl = data;
        }
        else {
          items.push({
            kind: 'alert',
            key: `${message.id}-acl-${partIndex}`,
            data,
          });
        }
      }
    });
  });

  return items;
});

function currentTerminalId() {
  return Number(connectionStore.terminalId) || 0;
}

function sendMessage(message: ChatMessage) {
  const socket = connectionStore.socket;
  if (!socket || socket.readyState !== WebSocket.OPEN) {
    throw new Error('Terminal WebSocket is not connected');
  }
  socket.send(buildJSONEnvelope(ENVELOPE_CHAT, message));
}

function scrollToBottom() {
  void nextTick(() => {
    const element = messagesElement.value;
    if (element)
      element.scrollTop = element.scrollHeight;
  });
}

function submit() {
  const text = input.value.trim();
  if (!text || busy.value)
    return;
  const terminalId = currentTerminalId();
  const message: ChatMessage = {
    id: `user-${Date.now()}-${Math.random().toString(16).slice(2)}`,
    role: 'user',
    metadata: { terminalId },
    parts: [{ type: 'text', text }],
  };
  input.value = '';
  errorText.value = '';
  messages.value.push(message);
  scrollToBottom();
  try {
    sendMessage(message);
    busy.value = true;
  }
  catch (error) {
    errorText.value = error instanceof Error ? error.message : '发送失败';
    busy.value = false;
  }
}

function updatePolicy() {
  try {
    sendMessage({
      id: `policy-${Date.now()}`,
      role: 'user',
      metadata: { terminalId: currentTerminalId() },
      parts: [
        {
          type: 'data-policy',
          data: {
            approvalThreshold: approvalThreshold.value,
            executionMode: executionMode.value,
          },
        },
      ],
    });
  }
  catch (error) {
    errorText.value = error instanceof Error ? error.message : '策略更新失败';
  }
}

function decide(data: EventData, approved: boolean) {
  if (decisions.value.has(String(data.id)))
    return;
  const next = new Set(decisions.value);
  next.add(String(data.id));
  decisions.value = next;
  try {
    sendMessage({
      id: `decision-${Date.now()}`,
      role: 'user',
      metadata: { terminalId: currentTerminalId() },
      parts: [
        {
          type: 'data-approval',
          data: {
            id: data.id,
            digest: data.digest,
            approved,
            execution: executionOverrides.value.get(String(data.id)) || data.execution,
          },
        },
      ],
    });
  }
  catch (error) {
    const rollback = new Set(decisions.value);
    rollback.delete(String(data.id));
    decisions.value = rollback;
    errorText.value = error instanceof Error ? error.message : '审批提交失败';
  }
}

function interrupt() {
  try {
    sendMessage({
      id: `interrupt-${Date.now()}`,
      role: 'user',
      metadata: { terminalId: currentTerminalId() },
      parts: [{ type: 'data-interrupt', data: { reason: 'user' } }],
    });
  }
  catch (error) {
    errorText.value = error instanceof Error ? error.message : '中断失败';
  }
}

function setExecutionOverride(id: string, value: string) {
  const next = new Map(executionOverrides.value);
  next.set(id, value);
  executionOverrides.value = next;
}

function handleIncoming(message: ChatMessage) {
  if (Number(message.metadata?.terminalId) !== currentTerminalId())
    return;
  const progress = message.parts.find(part => part.type === 'data-progress')?.data;
  if (progress) {
    runtimeStatus.value = String(progress.text || '');
    busy.value = String(progress.state || '') !== 'idle';
    return;
  }
  const capability = message.parts.find(part => part.type === 'data-capability')?.data;
  if (capability) {
    approvalThreshold.value = Number(capability.approvalThreshold) || 2;
    executionMode.value = String(capability.executionMode || 'auto');
    return;
  }
  if (message.parts.some(part => part.type === 'data-input-lock')) {
    return;
  }
  const policy = message.parts.find(part => part.type === 'data-policy')?.data;
  if (policy) {
    approvalThreshold.value = Number(policy.approvalThreshold) || approvalThreshold.value;
    executionMode.value = String(policy.executionMode || executionMode.value);
    return;
  }
  const runtimeError = message.parts.find(part => part.type === 'data-error')?.data;
  if (runtimeError) {
    errorText.value = String(runtimeError.message || 'Terminal AI failed');
    busy.value = false;
  }
  messages.value.push(message);
  scrollToBottom();
}

function renderMarkdown(source: string) {
  const html = marked.parse(source, {
    async: false,
    breaks: true,
    gfm: true,
  }) as string;
  return DOMPurify.sanitize(html);
}

function stepStatus(step: ViewStep) {
  const outcome = String(step.execution?.outcome || '');
  if (outcome)
    return outcome;
  const commandState = String(step.command?.state || '');
  if (commandState)
    return commandState;
  return step.status;
}

function statusLabel(step: ViewStep) {
  const labels: Record<string, string> = {
    approved: '已批准',
    auto_approved: '自动批准',
    awaiting_approval: '等待审批',
    awaiting_risk_approval: '等待审批',
    completed: '完成',
    error: '失败',
    executing: '执行中',
    failed: '失败',
    in_progress: '执行中',
    interrupted: '已中断',
    pending: '待执行',
    rejected: '已拒绝',
    reviewing: '分析结果',
    running: '执行中',
    success: '完成',
    succeeded: '完成',
  };
  const status = stepStatus(step);
  return labels[status] || status || '待执行';
}

function statusClass(step: ViewStep) {
  const status = stepStatus(step);
  if (['completed', 'success', 'succeeded'].includes(status))
    return 'is-success';
  if (['error', 'failed', 'rejected'].includes(status))
    return 'is-error';
  if (['awaiting_approval', 'awaiting_risk_approval'].includes(status))
    return 'is-warning';
  if (['approved', 'auto_approved', 'executing', 'in_progress', 'reviewing', 'running'].includes(status)) {
    return 'is-active';
  }
  return 'is-muted';
}

function statusIcon(step: ViewStep) {
  const status = statusClass(step);
  if (status === 'is-success')
    return CircleCheck;
  if (status === 'is-error' || status === 'is-warning')
    return CircleAlert;
  if (status === 'is-active')
    return LoaderCircle;
  return CircleDot;
}

function isStepExpanded(step: ViewStep) {
  const override = expansionOverrides.value.get(step.key);
  if (override !== undefined)
    return override;
  return ['is-active', 'is-error', 'is-warning'].includes(statusClass(step));
}

function toggleStep(step: ViewStep) {
  const next = new Map(expansionOverrides.value);
  next.set(step.key, !isStepExpanded(step));
  expansionOverrides.value = next;
}

function riskClass(level: number) {
  if (level >= 4)
    return 'risk-critical';
  if (level >= 3)
    return 'risk-high';
  if (level >= 2)
    return 'risk-medium';
  return 'risk-low';
}

onMounted(() => mittBus.on('terminal-ai-message', handleIncoming));
onUnmounted(() => mittBus.off('terminal-ai-message', handleIncoming));
</script>

<template>
  <section class="terminal-ai" :style="themeStyle">
    <header class="toolbar">
      <div class="assistant-title">
        <span class="assistant-mark"><Bot :size="16" /></span>
        <div>
          <strong>Terminal AI</strong>
          <small>基于当前终端上下文</small>
        </div>
      </div>
      <div class="policy-controls">
        <n-select
          v-model:value="approvalThreshold"
          size="small"
          :options="thresholdOptions"
          @update:value="updatePolicy"
        />
        <n-select
          v-model:value="executionMode"
          size="small"
          :options="modeOptions"
          :title="connectionStore.terminalAIBackgroundReason"
          @update:value="updatePolicy"
        />
      </div>
      <p
        v-if="!connectionStore.terminalAIBackground
          && connectionStore.terminalAIBackgroundReason"
        class="background-reason"
      >
        {{ connectionStore.terminalAIBackgroundReason }}
      </p>
    </header>

    <main ref="messagesElement" class="messages">
      <div v-if="messages.length === 0" class="empty-state">
        <span class="empty-icon"><SquareTerminal :size="22" /></span>
        <strong>准备协助当前终端</strong>
        <p>可以分析终端问题，也可以规划并执行操作。</p>
      </div>

      <template v-for="item in viewItems" :key="item.key">
        <article v-if="item.kind === 'text'" class="message" :class="[item.role]">
          <span class="avatar">
            <UserRound v-if="item.role === 'user'" :size="14" />
            <Bot v-else :size="14" />
          </span>
          <div class="message-content">
            <small>{{ item.role === 'user' ? '你' : 'Terminal AI' }}</small>
            <div class="markdown-body" v-html="renderMarkdown(item.text)" />
          </div>
        </article>

        <section v-else-if="item.kind === 'plan'" class="plan-card">
          <header class="plan-header">
            <span class="plan-icon"><SquareTerminal :size="16" /></span>
            <div>
              <small>执行计划</small>
              <div class="markdown-body compact" v-html="renderMarkdown(item.summary)" />
            </div>
          </header>

          <div class="plan-steps">
            <article
              v-for="step in item.steps"
              :key="step.key"
              class="step-card" :class="[statusClass(step)]"
            >
              <button class="step-toggle" type="button" @click="toggleStep(step)">
                <span class="step-number">{{ step.index }}</span>
                <span class="step-title">{{ step.title }}</span>
                <span class="status-badge" :class="[statusClass(step)]">
                  <component
                    :is="statusIcon(step)"
                    :class="{ spinning: statusClass(step) === 'is-active' }"
                    :size="13"
                  />
                  {{ statusLabel(step) }}
                </span>
                <ChevronDown v-if="isStepExpanded(step)" :size="15" />
                <ChevronRight v-else :size="15" />
              </button>

              <div v-if="isStepExpanded(step)" class="step-body">
                <div
                  v-if="step.objective"
                  class="markdown-body objective"
                  v-html="renderMarkdown(step.objective)"
                />

                <div v-if="step.command" class="command-section">
                  <div class="section-heading">
                    <span>命令</span>
                    <div class="badges">
                      <span class="badge" :class="[riskClass(Number(step.command.riskLevel))]">
                        风险 {{ step.command.riskLevel }}
                      </span>
                      <span class="badge">{{ step.command.execution }}</span>
                      <span v-if="step.command.state === 'auto_approved'" class="badge approved">
                        自动放行
                      </span>
                    </div>
                  </div>
                  <pre class="command-output"><code>{{ step.command.command }}</code></pre>
                  <div
                    v-if="step.command.rationale"
                    class="markdown-body command-note"
                    v-html="renderMarkdown(String(step.command.rationale))"
                  />
                  <p v-if="step.command.riskReason" class="risk-reason">
                    <CircleAlert :size="13" />
                    {{ step.command.riskReason }}
                  </p>

                  <div
                    v-if="
                      step.command.partType === 'data-approval'
                        && !decisions.has(String(step.command.id))
                    "
                    class="approval-panel"
                  >
                    <n-radio-group
                      v-if="executionMode === 'auto'"
                      :value="
                        executionOverrides.get(String(step.command.id))
                          || step.command.execution
                      "
                      @update:value="
                        (value: string) =>
                          setExecutionOverride(String(step.command?.id), String(value))
                      "
                    >
                      <n-radio value="pty">
                        当前 PTY
                      </n-radio>
                      <n-radio
                        value="background_exec"
                        :disabled="!connectionStore.terminalAIBackground
                          || step.command.backgroundEligible === false"
                      >
                        后台执行
                      </n-radio>
                    </n-radio-group>
                    <div class="approval-actions">
                      <n-button
                        size="small"
                        type="primary"
                        @click="decide(step.command || {}, true)"
                      >
                        批准
                      </n-button>
                      <n-button size="small" @click="decide(step.command || {}, false)">
                        拒绝
                      </n-button>
                    </div>
                  </div>
                </div>

                <div v-if="step.execution" class="result-section">
                  <div class="section-heading">
                    <span>执行结果</span>
                    <span
                      v-if="step.execution.exitCode !== undefined"
                      class="badge exit-code"
                    >
                      exit {{ step.execution.exitCode }}
                    </span>
                  </div>
                  <div
                    v-if="step.execution.summary"
                    class="markdown-body result-summary"
                    v-html="renderMarkdown(String(step.execution.summary))"
                  />
                  <pre
                    v-if="step.execution.output"
                    class="command-output result-output"
                  ><code>{{ step.execution.output }}</code></pre>
                  <p
                    v-if="!step.execution.summary && !step.execution.output"
                    class="result-placeholder"
                  >
                    {{ statusLabel(step) }}
                  </p>
                </div>

                <div v-if="step.acl" class="acl-notice">
                  <CircleAlert :size="14" />
                  命令 ACL：{{ step.acl.state || step.acl.action }}
                  {{ step.acl.decision?.name || step.acl.name || '' }}
                </div>
              </div>
            </article>
          </div>
        </section>

        <div v-else class="acl-notice standalone">
          <CircleAlert :size="14" />
          命令 ACL：{{ item.data.state || item.data.action }}
          {{ item.data.decision?.name || item.data.name || '' }}
        </div>
      </template>
    </main>

    <footer class="composer">
      <n-alert v-if="errorText" type="error" closable @close="errorText = ''">
        {{ errorText }}
      </n-alert>
      <div v-if="runtimeStatus" class="runtime-status">
        <LoaderCircle v-if="busy" class="spinning" :size="13" />
        <CircleDot v-else :size="13" />
        {{ runtimeStatus }}
      </div>
      <n-input
        v-model:value="input"
        type="textarea"
        :autosize="{ minRows: 2, maxRows: 5 }"
        placeholder="询问当前终端或描述要完成的任务"
        :disabled="busy"
        @keydown.enter.exact.prevent="submit"
      />
      <div class="composer-actions">
        <n-button v-if="busy" size="small" @click="interrupt">
          <template #icon>
            <Square :size="13" />
          </template>
          中断任务
        </n-button>
        <n-button size="small" type="primary" :disabled="busy || !input.trim()" @click="submit">
          <template #icon>
            <Send :size="13" />
          </template>
          发送
        </n-button>
      </div>
    </footer>
  </section>
</template>

<style scoped>
.terminal-ai {
  --ai-text: rgba(255, 255, 255, 0.92);
  --ai-text-muted: rgba(255, 255, 255, 0.58);
  display: flex;
  height: calc(100vh - 180px);
  min-height: 420px;
  flex-direction: column;
  color: var(--ai-text);
  background: var(--ai-bg);
}

.toolbar {
  padding: 12px;
  border-bottom: 1px solid var(--ai-border);
  background: var(--ai-surface);
}

.assistant-title,
.policy-controls,
.section-heading,
.badges,
.approval-actions,
.composer-actions,
.runtime-status,
.acl-notice {
  display: flex;
  align-items: center;
}

.assistant-title {
  gap: 9px;
  margin-bottom: 10px;
}

.assistant-title > div {
  display: flex;
  flex-direction: column;
}

.assistant-title strong {
  font-size: 13px;
  line-height: 18px;
}

.assistant-title small,
.plan-header small,
.message-content > small {
  color: var(--ai-text-muted);
  font-size: 11px;
}

.assistant-mark,
.empty-icon,
.plan-icon {
  display: grid;
  flex: none;
  place-items: center;
  border: 1px solid var(--ai-border);
  color: var(--ai-accent);
  background: var(--ai-accent-soft);
}

.assistant-mark {
  width: 30px;
  height: 30px;
  border-radius: 8px;
}

.policy-controls {
  gap: 8px;
}

.policy-controls > * {
  flex: 1;
  min-width: 0;
}

.background-reason {
  margin: 7px 0 0;
  color: var(--ai-text-muted);
  font-size: 11px;
}

.messages {
  flex: 1;
  min-height: 0;
  overflow: auto;
  padding: 14px 12px 18px;
  scrollbar-color: var(--ai-border) transparent;
}

.empty-state {
  display: flex;
  min-height: 190px;
  align-items: center;
  justify-content: center;
  flex-direction: column;
  color: var(--ai-text-muted);
  text-align: center;
}

.empty-state .empty-icon {
  width: 42px;
  height: 42px;
  margin-bottom: 12px;
  border-radius: 12px;
}

.empty-state strong {
  color: var(--ai-text);
  font-size: 13px;
}

.empty-state p {
  margin: 5px 0 0;
  font-size: 12px;
}

.message {
  display: flex;
  gap: 8px;
  margin-bottom: 14px;
}

.message.user {
  max-width: 88%;
  margin-left: auto;
  flex-direction: row-reverse;
}

.avatar {
  display: grid;
  width: 24px;
  height: 24px;
  flex: none;
  place-items: center;
  border: 1px solid var(--ai-border);
  border-radius: 7px;
  color: var(--ai-accent);
  background: var(--ai-surface-raised);
}

.message.user .avatar {
  background: var(--ai-accent-soft);
}

.message-content {
  min-width: 0;
}

.message.user .message-content {
  text-align: right;
}

.message-content .markdown-body {
  margin-top: 3px;
  padding: 8px 10px;
  border: 1px solid var(--ai-border);
  border-radius: 4px 10px 10px;
  background: var(--ai-surface-raised);
  text-align: left;
}

.message.user .markdown-body {
  border-radius: 10px 4px 10px 10px;
  background: var(--ai-accent-soft);
}

.plan-card {
  overflow: hidden;
  margin-bottom: 14px;
  border: 1px solid var(--ai-border);
  border-radius: 10px;
  background: var(--ai-surface);
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.14);
}

.plan-header {
  display: flex;
  align-items: flex-start;
  gap: 9px;
  padding: 11px 12px;
  border-bottom: 1px solid var(--ai-border);
}

.plan-icon {
  width: 28px;
  height: 28px;
  border-radius: 7px;
}

.plan-header > div {
  min-width: 0;
}

.plan-steps {
  padding: 8px;
}

.step-card {
  overflow: hidden;
  border: 1px solid transparent;
  border-radius: 8px;
  background: var(--ai-surface-raised);
}

.step-card + .step-card {
  margin-top: 6px;
}

.step-card.is-active,
.step-card.is-warning {
  border-color: var(--ai-border);
}

.step-card.is-error {
  border-color: rgba(248, 113, 113, 0.38);
}

.step-toggle {
  display: grid;
  width: 100%;
  min-height: 42px;
  align-items: center;
  padding: 7px 9px;
  border: 0;
  grid-template-columns: 22px minmax(0, 1fr) auto 16px;
  gap: 7px;
  color: inherit;
  background: transparent;
  cursor: pointer;
  font: inherit;
  text-align: left;
}

.step-toggle:hover {
  background: var(--ai-hover);
}

.step-number {
  display: grid;
  width: 20px;
  height: 20px;
  place-items: center;
  border: 1px solid var(--ai-border);
  border-radius: 6px;
  color: var(--ai-text-muted);
  font-size: 11px;
}

.step-title {
  overflow: hidden;
  font-size: 12px;
  font-weight: 600;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.status-badge,
.badge {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  border: 1px solid var(--ai-border);
  border-radius: 999px;
  color: var(--ai-text-muted);
  background: rgba(0, 0, 0, 0.1);
  white-space: nowrap;
}

.status-badge {
  padding: 2px 6px;
  font-size: 10px;
}

.status-badge.is-success {
  color: #86efac;
}

.status-badge.is-error {
  color: #fca5a5;
}

.status-badge.is-warning {
  color: #fcd34d;
}

.status-badge.is-active {
  color: var(--ai-accent);
}

.step-body {
  padding: 0 10px 10px 38px;
}

.objective {
  margin-bottom: 9px;
  color: var(--ai-text-muted);
}

.command-section,
.result-section {
  overflow: hidden;
  border: 1px solid var(--ai-border);
  border-radius: 7px;
  background: rgba(0, 0, 0, 0.12);
}

.result-section {
  margin-top: 7px;
}

.section-heading {
  min-height: 34px;
  justify-content: space-between;
  gap: 8px;
  padding: 6px 8px;
  border-bottom: 1px solid var(--ai-border);
  color: var(--ai-text-muted);
  font-size: 11px;
  font-weight: 600;
}

.badges {
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 4px;
}

.badge {
  padding: 1px 6px;
  font-size: 10px;
  font-weight: 400;
}

.badge.approved,
.risk-low {
  color: #86efac;
}

.risk-medium {
  color: #93c5fd;
}

.risk-high {
  color: #fcd34d;
}

.risk-critical {
  color: #fca5a5;
}

.command-output {
  overflow: auto;
  max-height: 280px;
  margin: 0;
  padding: 9px;
  color: rgba(255, 255, 255, 0.88);
  background: rgba(0, 0, 0, 0.2);
  font: 11px/1.55 ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  white-space: pre-wrap;
  word-break: break-word;
}

.command-note,
.result-summary {
  padding: 8px 9px 0;
  color: var(--ai-text-muted);
}

.risk-reason {
  display: flex;
  align-items: flex-start;
  gap: 5px;
  margin: 7px 9px 9px;
  color: #fcd34d;
  font-size: 11px;
}

.risk-reason svg {
  margin-top: 1px;
  flex: none;
}

.approval-panel {
  padding: 9px;
  border-top: 1px solid var(--ai-border);
}

.approval-actions {
  justify-content: flex-end;
  gap: 7px;
  margin-top: 8px;
}

.result-output {
  border-top: 0;
}

.result-placeholder {
  margin: 0;
  padding: 9px;
  color: var(--ai-text-muted);
  font-size: 11px;
}

.acl-notice {
  gap: 6px;
  padding: 8px 9px;
  color: #fcd34d;
  font-size: 11px;
  background: rgba(245, 158, 11, 0.08);
}

.acl-notice.standalone {
  margin-bottom: 12px;
  border: 1px solid rgba(245, 158, 11, 0.22);
  border-radius: 7px;
}

.composer {
  padding: 10px 12px 12px;
  border-top: 1px solid var(--ai-border);
  background: var(--ai-surface);
}

.runtime-status {
  gap: 5px;
  margin: 0 2px 7px;
  color: var(--ai-text-muted);
  font-size: 11px;
}

.composer-actions {
  justify-content: flex-end;
  gap: 7px;
  margin-top: 8px;
}

.markdown-body {
  overflow-wrap: anywhere;
  font-size: 12px;
  line-height: 1.65;
}

.markdown-body.compact {
  font-weight: 600;
  line-height: 1.45;
}

.markdown-body :deep(> :first-child) {
  margin-top: 0;
}

.markdown-body :deep(> :last-child) {
  margin-bottom: 0;
}

.markdown-body :deep(p) {
  margin: 0 0 7px;
}

.markdown-body :deep(ul),
.markdown-body :deep(ol) {
  margin: 5px 0;
  padding-left: 19px;
}

.markdown-body :deep(li + li) {
  margin-top: 3px;
}

.markdown-body :deep(a) {
  color: var(--ai-accent);
  text-decoration: none;
}

.markdown-body :deep(a:hover) {
  text-decoration: underline;
}

.markdown-body :deep(code) {
  padding: 1px 4px;
  border-radius: 4px;
  color: var(--ai-accent);
  background: rgba(0, 0, 0, 0.22);
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.92em;
}

.markdown-body :deep(pre) {
  overflow: auto;
  margin: 7px 0;
  padding: 9px;
  border: 1px solid var(--ai-border);
  border-radius: 6px;
  background: rgba(0, 0, 0, 0.22);
  white-space: pre-wrap;
}

.markdown-body :deep(pre code) {
  padding: 0;
  color: inherit;
  background: transparent;
}

.markdown-body :deep(blockquote) {
  margin: 7px 0;
  padding-left: 9px;
  border-left: 2px solid var(--ai-accent);
  color: var(--ai-text-muted);
}

.markdown-body :deep(table) {
  width: 100%;
  margin: 7px 0;
  border-collapse: collapse;
}

.markdown-body :deep(th),
.markdown-body :deep(td) {
  padding: 5px 7px;
  border: 1px solid var(--ai-border);
  text-align: left;
}

.spinning {
  animation: spin 1s linear infinite;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
