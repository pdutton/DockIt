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
