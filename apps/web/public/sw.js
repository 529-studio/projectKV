// Service Worker for Warded Web Push Notifications
// Strictly zero emoji per AGENTS.md Rule 1.

self.addEventListener('push', function (event) {
  if (!event.data) return;
  let payload;
  try {
    payload = event.data.json();
  } catch (_e) {
    payload = {
      title: 'CẢNH BÁO NGẬP NƯỚC / FLOOD ALERT',
      body: event.data.text(),
    };
  }

  const options = {
    body: payload.body || '',
    icon: '/wared-icon.png',
    badge: '/wared-icon.png',
    data: {
      url: payload.url || '/',
      road_key: payload.road_key || '',
      severity: payload.severity || '',
    },
    vibrate: [200, 100, 200],
    tag: payload.tag || 'warded-flood-alert',
    renotify: true,
  };

  event.waitUntil(self.registration.showNotification(payload.title || 'Warded', options));
});

self.addEventListener('notificationclick', function (event) {
  event.notification.close();
  const targetUrl = event.notification.data?.url || '/';

  event.waitUntil(
    clients.matchAll({ type: 'window', includeUncontrolled: true }).then(function (clientList) {
      for (const client of clientList) {
        if (client.url.includes(targetUrl) && 'focus' in client) {
          return client.focus();
        }
      }
      if (clients.openWindow) {
        return clients.openWindow(targetUrl);
      }
    })
  );
});
