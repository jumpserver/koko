<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue';

import mittBus from '@/utils/mittBus';
import { useConnectionStore } from '@/store/modules/useConnection';
import { buildJSONEnvelope, ENVELOPE_CHAT } from '@/websocket/envelope';

type ChatPart = {
  type: string;
  text?: string;
  data?: Record<string, any>;
};

type ChatMessage = {
  id: string;
  role: 'user' | 'assistant';
  metadata?: Record<string, any>;
  parts: ChatPart[];
};

const connectionStore = useConnectionStore();
const messages = ref<ChatMessage[]>([]);
const input = ref('');
const runtimeStatus = ref('');
const busy = ref(false);
const approvalThreshold = ref(2);
const executionMode = ref('auto');
const decisions = ref(new Set<string>());
const executionOverrides = ref(new Map<string, string>());
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

function submit() {
  const text = input.value.trim();
  if (!text || busy.value) return;
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
  try {
    sendMessage(message);
    busy.value = true;
  } catch (error) {
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
  } catch (error) {
    errorText.value = error instanceof Error ? error.message : '策略更新失败';
  }
}

function decide(data: Record<string, any>, approved: boolean) {
  if (decisions.value.has(String(data.id))) return;
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
  } catch (error) {
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
  } catch (error) {
    errorText.value = error instanceof Error ? error.message : '中断失败';
  }
}

function setExecutionOverride(id: string, value: string) {
  const next = new Map(executionOverrides.value);
  next.set(id, value);
  executionOverrides.value = next;
}

function handleIncoming(message: ChatMessage) {
  if (Number(message.metadata?.terminalId) !== currentTerminalId()) return;
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
}

function commandTone(level: number): 'error' | 'warning' | 'info' | 'success' {
  if (level >= 4) return 'error';
  if (level >= 3) return 'warning';
  if (level >= 2) return 'info';
  return 'success';
}

onMounted(() => mittBus.on('terminal-ai-message', handleIncoming));
onUnmounted(() => mittBus.off('terminal-ai-message', handleIncoming));
</script>

<template>
  <section class="terminal-ai">
    <header class="toolbar">
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
        @update:value="updatePolicy"
      />
    </header>

    <main class="messages">
      <n-empty v-if="messages.length === 0" description="我可以解释问题，也可以协助操作当前终端。" />
      <article v-for="message in messages" :key="message.id" :class="['message', message.role]">
        <small>{{ message.role === 'user' ? 'You' : 'AI' }}</small>
        <template v-for="(part, index) in message.parts" :key="index">
          <p v-if="part.type === 'text'" class="text">{{ part.text }}</p>

          <n-card v-else-if="part.type === 'data-plan'" size="small" class="event-card">
            <b>{{ part.data?.summary || '执行计划' }}</b>
            <ol>
              <li v-for="step in part.data?.steps || []" :key="step.id">
                {{ step.title }} · {{ step.status }}
              </li>
            </ol>
          </n-card>

          <n-card
            v-else-if="part.type === 'data-command' || part.type === 'data-approval'"
            size="small"
            class="event-card"
          >
            <div class="command-head">
              <n-tag :type="commandTone(Number(part.data?.riskLevel))" size="small">
                风险 {{ part.data?.riskLevel }}
              </n-tag>
              <n-tag size="small">{{ part.data?.execution }}</n-tag>
              <span v-if="part.data?.state === 'auto_approved'">自动放行</span>
            </div>
            <pre>{{ part.data?.command }}</pre>
            <p>{{ part.data?.rationale }}</p>
            <small>{{ part.data?.riskReason }}</small>
            <template
              v-if="
                part.type === 'data-approval' &&
                !decisions.has(String(part.data?.id))
              "
            >
              <n-radio-group
                v-if="executionMode === 'auto'"
                :value="executionOverrides.get(String(part.data?.id)) || part.data?.execution"
                @update:value="(value: string) => setExecutionOverride(String(part.data?.id), value)"
              >
                <n-radio value="pty">当前 PTY</n-radio>
                <n-radio
                  value="background_exec"
                  :disabled="!connectionStore.terminalAIBackground"
                >
                  后台执行
                </n-radio>
              </n-radio-group>
              <n-space class="approval-actions">
                <n-button size="small" type="primary" @click="decide(part.data || {}, true)">Approve</n-button>
                <n-button size="small" @click="decide(part.data || {}, false)">Reject</n-button>
              </n-space>
            </template>
          </n-card>

          <n-card v-else-if="part.type === 'data-execution'" size="small" class="event-card">
            <b>{{ part.data?.summary || part.data?.outcome }}</b>
            <pre v-if="part.data?.output">{{ part.data.output }}</pre>
            <small v-if="part.data?.exitCode !== undefined">exit {{ part.data.exitCode }}</small>
          </n-card>

          <n-alert v-else-if="part.type === 'data-command-acl'" type="warning" class="event-card">
            命令 ACL：{{ part.data?.state || part.data?.action }}
            {{ part.data?.decision?.name || part.data?.name || '' }}
          </n-alert>
        </template>
      </article>
    </main>

    <footer>
      <n-alert v-if="errorText" type="error" closable @close="errorText = ''">{{ errorText }}</n-alert>
      <small v-if="runtimeStatus" class="runtime-status">{{ runtimeStatus }}</small>
      <n-input
        v-model:value="input"
        type="textarea"
        :autosize="{ minRows: 2, maxRows: 5 }"
        placeholder="询问当前终端或描述要完成的任务"
        :disabled="busy"
        @keydown.enter.exact.prevent="submit"
      />
      <n-space justify="end">
        <n-button v-if="busy" size="small" @click="interrupt">中断任务</n-button>
        <n-button size="small" type="primary" :disabled="busy || !input.trim()" @click="submit">发送</n-button>
      </n-space>
    </footer>
  </section>
</template>

<style scoped>
.terminal-ai {
  display: flex;
  height: calc(100vh - 180px);
  min-height: 420px;
  flex-direction: column;
  gap: 12px;
}

.toolbar {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
}

.messages {
  flex: 1;
  overflow: auto;
}

.message {
  margin-bottom: 12px;
}

.message.user {
  margin-left: 15%;
}

.text {
  margin: 4px 0;
  white-space: pre-wrap;
}

.event-card {
  margin-top: 6px;
}

.command-head {
  display: flex;
  align-items: center;
  gap: 6px;
}

pre {
  overflow: auto;
  padding: 8px;
  border-radius: 4px;
  background: rgba(128, 128, 128, 0.12);
  white-space: pre-wrap;
}

.approval-actions,
footer {
  margin-top: 8px;
}

.runtime-status {
  display: block;
  margin-bottom: 6px;
}
</style>
