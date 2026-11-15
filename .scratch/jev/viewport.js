// The scroll offset, the viewport size and the first heading on screen, to check --visible against.
JSON.stringify({ y: Math.round(scrollY), h: innerHeight, firstHeading: ([...document.querySelectorAll('h1,h2,h3')].find(h => { const r = h.getBoundingClientRect(); return r.bottom > 0 && r.top < innerHeight; }) || {}).textContent })
