<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { api } from '@/api/client'
import RichBody from '@/components/RichBody.vue'
import { useSite } from '@/composables/useSite'

// Company mode, for someone with no projects yet: the company's own home page
// (set in site settings) and a note on how projects reach them.
const { siteName } = useSite()
const content = ref('')
const canEdit = ref(false)
const loaded = ref(false)

onMounted(async () => {
  try {
    const home = await api.getCompanyHome()
    content.value = home.content
    canEdit.value = home.can_edit
  } catch {
    // The note below still explains what to do.
  } finally {
    loaded.value = true
  }
})
</script>

<template>
  <div class="company-home mx-auto py-3" style="max-width: 48rem">
    <h1 class="h3 fw-bold mb-3">Welcome to {{ siteName }}</h1>
    <div v-if="content" class="card card-body mb-3">
      <RichBody :body="content" />
    </div>
    <div class="alert alert-info d-flex gap-2 align-items-start" role="status">
      <i class="bi bi-info-circle mt-1" aria-hidden="true" />
      <div>
        <strong>You're not on any projects yet.</strong>
        Every task here belongs to a project. Once a project is shared with you or one of your teams, it shows up in
        the sidebar and you can start adding tasks.
      </div>
    </div>
    <p v-if="loaded && canEdit" class="small text-muted mb-0">
      <template v-if="!content">You can add a welcome message here for people who haven't joined a project yet. </template>
      Edit this page in <RouterLink to="/admin">Site settings</RouterLink>.
    </p>
  </div>
</template>
