# Task setup: edge-form. Writes the registration form the task fills.
# The page is served from the task's scratch folder by the runner's task
# server; submitting POSTs the field values to it, which is the end state
# the Check inspects.
$scratch = $env:WINUSE_TASK_SCRATCH
New-Item -ItemType Directory -Force -Path $scratch | Out-Null

$html = @'
<!doctype html>
<html>
<head>
<meta charset="utf-8">
<title>Team registration</title>
<style>
  body { font: 16px system-ui, sans-serif; margin: 40px auto; max-width: 480px; }
  label { display: block; margin: 14px 0 2px; font-weight: 600; }
  input[type=text], input[type=email], select, textarea { width: 100%; padding: 6px; }
  .radio { margin: 4px 0; }
  button { margin-top: 18px; padding: 8px 28px; }
</style>
</head>
<body>
<h1>Team registration</h1>
<form id="reg">
  <label for="name">Full name</label>
  <input type="text" id="name" name="name" required>

  <label for="email">Email</label>
  <input type="email" id="email" name="email" required>

  <label for="team">Team</label>
  <select id="team" name="team">
    <option value="">Choose a team</option>
    <option value="Aurora">Aurora</option>
    <option value="Borealis">Borealis</option>
    <option value="Cascade">Cascade</option>
  </select>

  <label>Role</label>
  <div class="radio"><input type="radio" id="dev" name="role" value="Developer"> <label for="dev" style="display:inline;font-weight:400">Developer</label></div>
  <div class="radio"><input type="radio" id="ops" name="role" value="Operations"> <label for="ops" style="display:inline;font-weight:400">Operations</label></div>

  <label><input type="checkbox" id="newsletter" name="newsletter" style="width:auto"> Send me the newsletter</label>

  <label for="notes">Notes</label>
  <textarea id="notes" name="notes" rows="3"></textarea>

  <button type="submit" id="submit">Register</button>
</form>
<div id="done" hidden>
  <h2>Thank you</h2>
  <p>Your registration was received.</p>
</div>
<script>
document.getElementById('reg').addEventListener('submit', function (e) {
  e.preventDefault();
  var role = document.querySelector('input[name=role]:checked');
  var data = {
    name: document.getElementById('name').value,
    email: document.getElementById('email').value,
    team: document.getElementById('team').value,
    role: role ? role.value : '',
    newsletter: document.getElementById('newsletter').checked,
    notes: document.getElementById('notes').value
  };
  fetch(location.origin + '/log', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(data)
  }).then(function () {
    document.getElementById('reg').hidden = true;
    document.getElementById('done').hidden = false;
  }).catch(function () {
    alert('Could not submit - is the task server running?');
  });
});
</script>
</body>
</html>
'@
$utf8 = New-Object System.Text.UTF8Encoding($false)
[System.IO.File]::WriteAllText((Join-Path $scratch 'form.html'), $html, $utf8)
exit 0
