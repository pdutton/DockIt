// Show timestamps, sent in UTC, in the browser's local time.  Without
// JavaScript the UTC text stays, which is still correct.
//
// A time with class "short" is in a list, where width matters: it drops the
// year when it is this year, and the time of day otherwise.  Its tooltip has
// the full local time.
document.addEventListener("DOMContentLoaded", () => {
  const full = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" });
  const thisYear = new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" });
  const otherYear = new Intl.DateTimeFormat(undefined, { dateStyle: "medium" });
  const year = new Date().getFullYear();
  for (const el of document.querySelectorAll("time[datetime]")) {
    const d = new Date(el.getAttribute("datetime"));
    if (isNaN(d)) {
      continue;
    }
    if (el.classList.contains("short")) {
      el.title = full.format(d);
      el.textContent = (d.getFullYear() === year ? thisYear : otherYear).format(d);
    } else {
      el.title = el.textContent;
      el.textContent = full.format(d);
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
