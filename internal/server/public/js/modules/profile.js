import { apiPath } from "./utils.js";
import { showToast } from "./notifications.js";

function setButtonLoading(btn, loading, defaultHtml) {
  if (!btn) return;
  btn.disabled = loading;
  if (loading) {
    btn.dataset.defaultHtml = btn.innerHTML;
    btn.innerHTML =
      '<span class="spinner-border spinner-border-sm me-2" role="status" aria-hidden="true"></span>Saving…';
  } else {
    btn.innerHTML = btn.dataset.defaultHtml || defaultHtml;
  }
}

export function initProfilePage() {
  document.body.addEventListener("click", (e) => {
    const btn = e.target.closest("#copy-calendar-url");
    if (!btn) return;
    const url =
      btn.dataset.url ||
      (document.getElementById("calendar-feed-url") || {}).value;
    if (!url) return;
    navigator.clipboard
      .writeText(url)
      .then(() => {
        btn.innerHTML = '<i class="bi bi-check-lg"></i> Copied';
        showToast("Calendar URL copied to clipboard.");
        setTimeout(() => {
          btn.innerHTML = '<i class="bi bi-clipboard"></i> Copy';
        }, 2000);
      })
      .catch(() => {
        showToast("Could not copy to clipboard.", { error: true });
      });
  });

  const regenBtn = document.getElementById("regenerate-calendar-token");
  if (regenBtn) {
    regenBtn.addEventListener("htmx:afterRequest", (e) => {
      if (e.detail.successful) {
        showToast("Calendar link regenerated.");
      }
    });
  }

  const profileForm = document.getElementById("profileForm");
  if (profileForm) {
    profileForm.addEventListener("submit", async (e) => {
      e.preventDefault();
      const submitBtn = profileForm.querySelector('button[type="submit"]');
      setButtonLoading(
        submitBtn,
        true,
        '<i class="bi bi-check-lg"></i> Save Changes',
      );
      const formData = new FormData();
      formData.append("timezone", document.getElementById("timezone").value);
      formData.append("user_name", document.getElementById("user_name").value);
      const per = document.getElementById("items_per_page");
      if (per) formData.append("items_per_page", per.value);
      try {
        const response = await fetch(apiPath("/api/update-profile"), {
          method: "POST",
          headers: { "HX-Request": "true" },
          body: formData,
        });
        if (response.ok) {
          const successMsg = document.getElementById("successMessage");
          successMsg?.classList.remove("d-none");
          successMsg?.classList.add("show");
          showToast("Profile updated successfully.");
          setTimeout(() => {
            successMsg?.classList.remove("show");
            successMsg?.classList.add("d-none");
          }, 3000);
        } else {
          document.getElementById("errorText").textContent =
            "Error updating profile. Please try again.";
          document.getElementById("errorMessage").style.display = "block";
          showToast("Error updating profile. Please try again.", { error: true });
        }
      } catch {
        document.getElementById("errorText").textContent =
          "An error occurred. Please try again.";
        document.getElementById("errorMessage").style.display = "block";
        showToast("An error occurred. Please try again.", { error: true });
      } finally {
        setButtonLoading(
          submitBtn,
          false,
          '<i class="bi bi-check-lg"></i> Save Changes',
        );
      }
    });
  }

  const passwordForm = document.getElementById("passwordForm");
  if (passwordForm) {
    passwordForm.addEventListener("submit", async (e) => {
      e.preventDefault();
      const submitBtn = passwordForm.querySelector('button[type="submit"]');
      const newPassword = document.getElementById("new_password").value;
      const confirmPassword = document.getElementById("confirm_password").value;
      if (newPassword !== confirmPassword) {
        document.getElementById("passwordErrorText").textContent =
          "New passwords do not match.";
        document.getElementById("passwordErrorMessage").style.display = "block";
        showToast("New passwords do not match.", { error: true });
        return;
      }
      setButtonLoading(
        submitBtn,
        true,
        '<i class="bi bi-key"></i> Change Password',
      );
      const formData = new FormData();
      formData.append(
        "current_password",
        document.getElementById("current_password").value,
      );
      formData.append("new_password", newPassword);
      formData.append("confirm_password", confirmPassword);
      try {
        const response = await fetch(apiPath("/api/change-password"), {
          method: "POST",
          headers: { "HX-Request": "true" },
          body: formData,
        });
        if (response.ok) {
          const responseText = await response.text();
          if (responseText === "success") {
            const successMsg = document.getElementById("passwordSuccessMessage");
            successMsg?.classList.remove("d-none");
            successMsg?.classList.add("show");
            document.getElementById("passwordErrorMessage").style.display =
              "none";
            passwordForm.reset();
            showToast("Password changed successfully.");
            setTimeout(() => {
              successMsg?.classList.remove("show");
              successMsg?.classList.add("d-none");
            }, 3000);
          } else {
            document.getElementById("passwordErrorText").textContent =
              responseText;
            document.getElementById("passwordErrorMessage").style.display =
              "block";
            showToast(responseText, { error: true });
          }
        } else {
          document.getElementById("passwordErrorText").textContent =
            "Error changing password. Please try again.";
          document.getElementById("passwordErrorMessage").style.display =
            "block";
          showToast("Error changing password. Please try again.", {
            error: true,
          });
        }
      } catch {
        document.getElementById("passwordErrorText").textContent =
          "An error occurred. Please try again.";
        document.getElementById("passwordErrorMessage").style.display = "block";
        showToast("An error occurred. Please try again.", { error: true });
      } finally {
        setButtonLoading(
          submitBtn,
          false,
          '<i class="bi bi-key"></i> Change Password',
        );
      }
    });
  }
}
