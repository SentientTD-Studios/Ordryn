<script setup lang="ts">
import { computed } from 'vue'
import type { RecurrenceFrequency } from '@/api/types'
import { recurrenceSummary, WEEKDAY_SHORT, type RecurrenceDraft } from '@/utils/recurrence'

const props = defineProps<{
  modelValue: RecurrenceDraft
  readOnly?: boolean
  /** Server preview of the next occurrence's due date (saved rule only). */
  nextDue?: string
  dueDate?: string
}>()

const emit = defineEmits<{ 'update:modelValue': [value: RecurrenceDraft] }>()

const units: Record<RecurrenceFrequency, string> = { daily: 'day(s)', weekly: 'week(s)', monthly: 'month(s)', yearly: 'year(s)' }
const summary = computed(() => recurrenceSummary(props.modelValue))

function update(patch: Partial<RecurrenceDraft>) {
  emit('update:modelValue', { ...props.modelValue, ...patch })
}

function toggleWeekday(day: number) {
  const set = new Set(props.modelValue.weekdays)
  if (set.has(day)) set.delete(day)
  else set.add(day)
  update({ weekdays: [...set].sort((a, b) => a - b) })
}
</script>

<template>
  <div class="task-recurrence-editor">
    <div class="form-check form-switch mb-1">
      <input
        id="recurrence_enabled"
        class="form-check-input"
        type="checkbox"
        :checked="modelValue.enabled"
        :disabled="readOnly"
        @change="update({ enabled: ($event.target as HTMLInputElement).checked })"
      />
      <label class="form-check-label" for="recurrence_enabled">
        <i class="bi bi-arrow-repeat me-1" />Repeat
      </label>
    </div>

    <template v-if="modelValue.enabled">
      <div class="d-flex align-items-center gap-2 flex-wrap mb-2">
        <span class="small">Every</span>
        <input
          type="number"
          min="1"
          max="365"
          class="form-control form-control-sm"
          style="width: 5rem"
          aria-label="Repeat interval"
          :value="modelValue.interval"
          :disabled="readOnly"
          @input="update({ interval: Number(($event.target as HTMLInputElement).value) || 1 })"
        />
        <select
          class="form-select form-select-sm w-auto"
          aria-label="Repeat frequency"
          :value="modelValue.frequency"
          :disabled="readOnly"
          @change="update({ frequency: ($event.target as HTMLSelectElement).value as RecurrenceFrequency })"
        >
          <option v-for="(label, freq) in units" :key="freq" :value="freq">{{ label }}</option>
        </select>
      </div>

      <div v-if="modelValue.frequency === 'weekly'" class="btn-group btn-group-sm flex-wrap mb-2" role="group" aria-label="Repeat on weekdays">
        <button
          v-for="(name, day) in WEEKDAY_SHORT"
          :key="day"
          type="button"
          class="btn"
          :class="modelValue.weekdays.includes(day) ? 'btn-primary' : 'btn-outline-secondary'"
          :aria-pressed="modelValue.weekdays.includes(day)"
          :disabled="readOnly"
          @click="toggleWeekday(day)"
        >
          {{ name }}
        </button>
      </div>

      <div v-if="modelValue.frequency === 'monthly'" class="d-flex align-items-center gap-2 mb-2">
        <label class="small mb-0" for="recurrence_month_day">On day</label>
        <select
          id="recurrence_month_day"
          class="form-select form-select-sm w-auto"
          :value="modelValue.monthDay"
          :disabled="readOnly"
          @change="update({ monthDay: Number(($event.target as HTMLSelectElement).value) })"
        >
          <option :value="0">Same as due date</option>
          <option v-for="d in 30" :key="d" :value="d">{{ d }}</option>
          <option :value="31">Last day</option>
        </select>
      </div>

      <div class="d-flex align-items-center gap-2 flex-wrap mb-2">
        <label class="small mb-0" for="recurrence_basis">Next due</label>
        <select
          id="recurrence_basis"
          class="form-select form-select-sm w-auto"
          :value="modelValue.basis"
          :disabled="readOnly"
          @change="update({ basis: ($event.target as HTMLSelectElement).value as RecurrenceDraft['basis'] })"
        >
          <option value="due">counts from the due date</option>
          <option value="completion">counts from when it's completed</option>
        </select>
      </div>

      <div class="d-flex align-items-center gap-2 flex-wrap mb-1">
        <label class="small mb-0" for="recurrence_end">Ends</label>
        <select
          id="recurrence_end"
          class="form-select form-select-sm w-auto"
          :value="modelValue.endMode"
          :disabled="readOnly"
          @change="update({ endMode: ($event.target as HTMLSelectElement).value as RecurrenceDraft['endMode'] })"
        >
          <option value="never">Never</option>
          <option value="on">On date</option>
          <option value="after">After</option>
        </select>
        <input
          v-if="modelValue.endMode === 'on'"
          type="date"
          class="form-control form-control-sm w-auto"
          aria-label="End date"
          :value="modelValue.endsOn"
          :disabled="readOnly"
          @input="update({ endsOn: ($event.target as HTMLInputElement).value })"
        />
        <template v-if="modelValue.endMode === 'after'">
          <input
            type="number"
            min="1"
            max="1000"
            class="form-control form-control-sm"
            style="width: 5rem"
            aria-label="Total occurrences"
            :value="modelValue.endAfter"
            :disabled="readOnly"
            @input="update({ endAfter: Number(($event.target as HTMLInputElement).value) || 1 })"
          />
          <span class="small">occurrences</span>
        </template>
      </div>

      <p class="text-muted small mb-0">
        {{ summary }}.
        <template v-if="nextDue"> Completing this creates the next one due {{ nextDue }}.</template>
        <template v-else-if="!dueDate"> No due date set — the next one is scheduled from the day it's completed.</template>
      </p>
    </template>
  </div>
</template>
