// Show timestamps, sent in UTC, in the browser's local time.  Without
// JavaScript the UTC text stays, which is still correct.
document.addEventListener("DOMContentLoaded", () => {
  const fmt = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" });
  for (const el of document.querySelectorAll("time[datetime]")) {
    const d = new Date(el.getAttribute("datetime"));
    if (!isNaN(d)) {
      el.title = el.textContent;
      el.textContent = fmt.format(d);
    }
  }
});

// Send a form once.  A second click while the first submission is on its way
// is ignored; the server would answer it with the first one's response
// anyway, but this spares the request and shows the form is busy.
document.addEventListener("submit", (e) => {
  const form = e.target;
  if (form.method !== "post") {
    return;
  }
  if (form.getAttribute("aria-busy") === "true") {
    e.preventDefault();
    return;
  }
  form.setAttribute("aria-busy", "true");
});

// A page restored by the back button comes back as it was left, busy.
window.addEventListener("pageshow", (e) => {
  if (e.persisted) {
    for (const form of document.querySelectorAll('form[aria-busy="true"]')) {
      form.removeAttribute("aria-busy");
    }
  }
});
