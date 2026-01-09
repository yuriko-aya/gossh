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
            // Read private key file and convert to base64
            const reader = new FileReader();
            reader.onload = function(event) {
                const privateKeyContent = event.target.result;
                privateKeyBase64 = btoa(privateKeyContent);
                
                // Open terminal in a full-screen popup window
                openTerminalPopup(host, user, password, privateKeyBase64);
            };
            reader.readAsText(privateKeyFile);
        } else {
            // Open terminal in a full-screen popup window
            openTerminalPopup(host, user, password, privateKeyBase64);
        }
    });
});

function openTerminalPopup(host, user, password, privatekey) {
    const params = new URLSearchParams({
        host: host,
        user: user,
        password: password,
        privatekey: privatekey
    });
    
    // Open popup window with 960x640 size
    const popup = window.open(
        `/terminal?${params.toString()}`,
        'SSH Terminal',
        'width=960,height=640,location=no,menubar=no,toolbar=no,status=no,resizable=yes'
    );
    
    if (popup) {
        popup.focus();
    } else {
        alert('Popup blocked! Please allow popups for this site.');
    }
}
