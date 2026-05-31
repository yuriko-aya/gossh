// Wait for DOM to be fully loaded
document.addEventListener('DOMContentLoaded', function() {
    const form = document.getElementById('sshForm');
    
    if (!form) {
        return; // Exit if form doesn't exist (e.g., on terminal page)
    }
    
    // Form submission handler
    form.addEventListener('submit', async function(e) {
        e.preventDefault();
        
        const host = document.getElementById('host').value;
        const user = document.getElementById('user').value;
        const password = document.getElementById('password').value;
        const privateKeyFile = document.getElementById('privatekey').files[0];
        
        let privateKeyBase64 = '';
        
        if (privateKeyFile) {
            privateKeyBase64 = await new Promise((resolve) => {
                const reader = new FileReader();
                reader.onload = function(event) {
                    resolve(btoa(event.target.result));
                };
                reader.readAsText(privateKeyFile);
            });
        }
        
        await openTerminalPopup(host, user, password, privateKeyBase64);
    });
});

async function openTerminalPopup(host, user, password, privatekey) {
    // Exchange credentials for a server-issued access token so that
    // sensitive values never appear in plain text in any URL.
    let accessToken;
    try {
        const body = new URLSearchParams({ host, user, password, privatekey });
        const response = await fetch('/connect', {
            method: 'POST',
            headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
            body: body.toString()
        });
        if (!response.ok) {
            alert('Connection failed: server returned ' + response.status);
            return;
        }
        const data = await response.json();
        if (!data.access) {
            alert('Connection failed: no access token returned.');
            return;
        }
        accessToken = data.access;
    } catch (err) {
        alert('Connection failed: ' + err.message);
        return;
    }

    // Open popup window with 960x640 size
    const popup = window.open(
        `/terminal?access=${encodeURIComponent(accessToken)}`,
        'SSH Terminal',
        'width=960,height=640,location=no,menubar=no,toolbar=no,status=no,resizable=yes'
    );
    
    if (popup) {
        popup.focus();
    } else {
        alert('Popup blocked! Please allow popups for this site.');
    }
}
