<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api } from '@/api/client'
import type {
  EmailMode,
  NotificationEmailSettings,
  NotificationPreference,
  NotificationPreferencesUpdate,
  ReminderTiming,
} from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'

const { push } = useToast()
const prefs = ref<NotificationPreference[]>([])
const email = ref<NotificationEmailSettings>({
  available: false,
  mode: 'off',
  digest_hour: 8,
  reminder_timing: 'both',
})
const loading = ref(true)
const saving = ref(false)

const hourOptions = Array.from({ length: 24 }, (_, h) => ({
  value: h,
  label: `${String(h).padStart(2, '0')}:00`,
}))

const emailOff = computed(() => email.value.mode === 'off')

async function load() {
  try {
    const res = await api.getNotificationPreferences()
    prefs.value = res.preferences
    email.value = res.email
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Failed to load notification settings', 'error')
  } finally {
    loading.value = false
  }
}

// Every control saves on change; the screen reloads from the response, or
// from the server again if the save fails, so it never shows unsaved state.
async function save(update: NotificationPreferencesUpdate) {
  saving.value = true
  try {
    const res = await api.updateNotificationPreferences(update)
    prefs.value = res.preferences
    email.value = res.email
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Update failed', 'error')
    await load()
  } finally {
    saving.value = false
  }
}

function toggleInApp(pref: NotificationPreference, enabled: boolean) {
  pref.enabled = enabled
  void save({ preferences: { [pref.type]: enabled } })
}

function toggleEmail(pref: NotificationPreference, on: boolean) {
  pref.email = on
  void save({ email_types: { [pref.type]: on } })
}

function setMode(mode: EmailMode) {
  email.value.mode = mode
  void save({ email_mode: mode })
}

function setDigestHour(hour: number) {
  email.value.digest_hour = hour
  void save({ digest_hour: hour })
}

function setReminderTiming(timing: ReminderTiming) {
  email.value.reminder_timing = timing
  void save({ reminder_timing: timing })
}

function emailSwitchTitle(pref: NotificationPreference): string {
  if (!pref.email_available) return 'This notification is not emailed on its own'
  if (!pref.enabled) return 'Turn on the in-app notification to email it'
  if (emailOff.value) return 'Choose an email delivery option above first'
  return ''
}

onMounted(() => {
  void load()
})
</script>

<template>
  <div class="card mb-4">
    <div class="card-header">
      <h3 class="card-title mb-0">Notifications</h3>
    </div>
    <div class="card-body">
      <p class="text-muted small mb-3">
        Choose which notifications you receive, in the app and by email. Changes save immediately. Security emails
        such as password resets and password-change confirmations are always sent.
      </p>
      <div v-if="loading" class="text-muted small">Loading…</div>

      <template v-else>
        <div
          v-if="!email.available"
          class="alert alert-secondary small py-2"
          role="status"
        >
          Email notifications aren't available yet. A site administrator needs to turn them on. You can choose your
          settings now; nothing is emailed until then.
        </div>

        <div class="row g-3 mb-4">
          <div class="col-sm-6">
            <label class="form-label fw-semibold" for="notif-email-mode">Email</label>
            <select
              id="notif-email-mode"
              class="form-select"
              :value="email.mode"
              :disabled="saving"
              @change="setMode(($event.target as HTMLSelectElement).value as EmailMode)"
            >
              <option value="off">Off</option>
              <option value="instant">As they happen</option>
              <option value="digest">Daily summary</option>
            </select>
            <div class="form-text">
              <template v-if="email.mode === 'instant'">
                Unread notifications are grouped into one email at most every 10 minutes. Anything you've already
                seen in the app isn't emailed.
              </template>
              <template v-else-if="email.mode === 'digest'">
                One email a day with your unread notifications, in your timezone.
              </template>
              <template v-else>Notifications stay in the app.</template>
            </div>
          </div>
          <div v-if="email.mode === 'digest'" class="col-sm-6">
            <label class="form-label fw-semibold" for="notif-digest-hour">Send the summary at</label>
            <select
              id="notif-digest-hour"
              class="form-select"
              :value="email.digest_hour"
              :disabled="saving"
              @change="setDigestHour(Number(($event.target as HTMLSelectElement).value))"
            >
              <option v-for="h in hourOptions" :key="h.value" :value="h.value">{{ h.label }}</option>
            </select>
          </div>
          <div class="col-sm-6">
            <label class="form-label fw-semibold" for="notif-reminder-timing">Due date reminders</label>
            <select
              id="notif-reminder-timing"
              class="form-select"
              :value="email.reminder_timing"
              :disabled="saving"
              @change="setReminderTiming(($event.target as HTMLSelectElement).value as ReminderTiming)"
            >
              <option value="both">The day before and the morning it's due</option>
              <option value="day_before">The day before</option>
              <option value="morning">The morning it's due</option>
              <option value="off">Off</option>
            </select>
            <div class="form-text">Sent at 08:00 in your timezone (set in Preferences).</div>
          </div>
        </div>

        <div class="table-responsive">
          <table class="table table-sm align-middle mb-0 notif-pref-table">
            <thead>
              <tr>
                <th scope="col">Notification</th>
                <th scope="col" class="text-center notif-col">In app</th>
                <th scope="col" class="text-center notif-col">Email</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="pref in prefs" :key="pref.type">
                <td>
                  <div class="fw-medium">{{ pref.label }}</div>
                  <div class="form-text mt-0">{{ pref.description }}</div>
                </td>
                <td class="text-center">
                  <div class="form-check form-switch d-inline-block m-0">
                    <input
                      :id="`notif-pref-${pref.type}`"
                      class="form-check-input"
                      type="checkbox"
                      role="switch"
                      :checked="pref.enabled"
                      :disabled="saving"
                      :aria-label="`${pref.label} in app`"
                      @change="toggleInApp(pref, ($event.target as HTMLInputElement).checked)"
                    />
                  </div>
                </td>
                <td class="text-center">
                  <span v-if="!pref.email_available" class="text-muted" :title="emailSwitchTitle(pref)">—</span>
                  <div v-else class="form-check form-switch d-inline-block m-0" :title="emailSwitchTitle(pref)">
                    <input
                      :id="`notif-email-${pref.type}`"
                      class="form-check-input"
                      type="checkbox"
                      role="switch"
                      :checked="pref.email && pref.enabled"
                      :disabled="saving || !pref.enabled || emailOff"
                      :aria-label="`${pref.label} by email`"
                      @change="toggleEmail(pref, ($event.target as HTMLInputElement).checked)"
                    />
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </template>
    </div>
  </div>
</template>

<style scoped>
.notif-col {
  width: 5.5rem;
}
</style>
