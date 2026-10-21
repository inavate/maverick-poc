// Monkey-patch Maverick HostedFields class to prevent "Uncaught TypeError: event.data is not iterable"
// when receiving message events from sources other than the Maverick iframe (e.g., devtools, browser extensions).
if (typeof HostedFields !== 'undefined' && HostedFields.prototype && HostedFields.prototype.onIframeMessage) {
    const originalOnIframeMessage = HostedFields.prototype.onIframeMessage;
    HostedFields.prototype.onIframeMessage = function (event) {
        if (!event || !Array.isArray(event.data)) {
            return; // Ignore non-array message events
        }
        return originalOnIframeMessage.call(this, event);
    };
}

var state = {
    maverickToken: '',
    cardToken: '',
    sdkForm: null
};

// Initialize Hosted Fields when the page loads
window.addEventListener('DOMContentLoaded', async () => {
    setupFormValidationInterceptor();
    await initializeHostedFields();
});

function setupFormValidationInterceptor() {
    const submitWrapper = document.getElementById('submit-btn-wrapper');
    if (submitWrapper && !submitWrapper.dataset.listenerAttached) {
        submitWrapper.dataset.listenerAttached = 'true';
        submitWrapper.addEventListener('click', (e) => {
            const amount = parseFloat(document.getElementById('payment-amount')?.value);
            const feeInput = document.getElementById('payment-fee');
            const fee = feeInput ? parseFloat(feeInput.value) : 0;

            if (!amount || isNaN(amount) || amount <= 0) {
                showError('Please enter a valid payment amount.');
                e.stopPropagation();
                e.preventDefault();
                return;
            }

            if (feeInput && !isNaN(fee) && fee < 0) {
                showError('Fee cannot be negative.');
                e.stopPropagation();
                e.preventDefault();
                return;
            }
        }, true); // Capture phase ensures pre-validation before Maverick iframe receives click
    }
}

async function initializeHostedFields() {
    showError('');
    showSuccess('');

    try {
        // 1. Fetch Session/Hosted Fields Token from Customer API
        const resp = await fetch('/api/v1/customer/maverick/init-payment', { method: 'POST' });
        if (!resp.ok) {
            const errData = await resp.json();
            throw new Error(errData.error || 'Failed to initialize session');
        }
        const data = await resp.json();
        state.maverickToken = data.maverick_token;

        // 2. Render and configure Maverick Hosted Fields
        state.sdkForm = HostedFields.create({
            token: state.maverickToken,
            amount: 0, // Setup as Add Payment Method Form to secure card token first
            fields: {
                "cardNumber": { target: "#card-number", useTargetStyle: false },
                "cardExpiration": { target: "#card-expiration", useTargetStyle: false },
                "cardCvv": { target: "#card-cvv", useTargetStyle: false },
                "cardHolderName": { target: "#card-holder-name", useTargetStyle: false },
                "submit": { target: "#submit-button" }
            },
            styles: {
                "input": {
                    "width": "100%",
                    "height": "38px",
                    "padding": "0.45rem 0.6rem",
                    "border-radius": "4px",
                    "border": "1px solid #d1d5db",
                    "font-size": "0.85rem",
                    "font-family": "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif",
                    "background": "#fff",
                    "box-sizing": "border-box"
                },
                "input:focus": {
                    "outline": "none",
                    "border-radius": "4px",
                    "border": "1px solid #2563eb",
                    "box-shadow": "0 0 0 2px rgba(37, 99, 235, 0.15)"
                },
                "input.invalid": {
                    "border-color": "#dc2626"
                },
                "input.valid": {
                    "border-color": "#16a34a"
                }
            }
        });



        // 3. Listen to Event callbacks
        state.sdkForm.addEventListener('submit.processing', () => {
            showError('');
            showSuccess('Verifying card and initiating payment flow...');

            // Show processing state on the custom overlay button
            const btn = document.getElementById('custom-pay-button');
            if (btn) {
                btn.disabled = true;
                btn.innerHTML = '<span class="loading-spinner"></span>Processing...';
            }
            // Disable overlay interactions
            const submitWrapper = document.getElementById('submit-button');
            if (submitWrapper) {
                submitWrapper.style.pointerEvents = 'none';
            }
        });

        state.sdkForm.addEventListener('submit.result', async (e) => {
            const success = e.detail.result;
            if (success) {
                showSuccess('Card linked! Exchanging token...');
                await saveCard();
            } else {
                showError('Card validation failed. Please check details.');
                resetButtonState();
            }
        });

        state.sdkForm.addEventListener('hostedFields.error', (e) => {
            showError('SDK Error: ' + e.detail.message);
            resetButtonState();
        });

    } catch (err) {
        showError(err.message);
        resetButtonState();
    }
}

async function saveCard() {
    try {
        // Send maverick session token to exchange for persistent token
        const resp = await fetch('/api/v1/customer/maverick/save-card', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ maverick_token: state.maverickToken })
        });

        if (!resp.ok) {
            const errData = await resp.json().catch(() => ({}));
            const errMsg = errData.error_description || errData.message || errData.reason || errData.error || 'Failed to retrieve card token';
            throw new Error(errMsg);
        }

        const data = await resp.json();
        state.cardToken = data.card_token;

        console.log("[saveCard] Received card data from server:", data);
        showSuccess(`Card linked successfully! Card: **** **** **** ${data.last_4 || 'XXXX'} (BIN: ${data.bin || 'N/A'}, EXP: ${data.exp || 'N/A'}). Processing payment...`);
        await chargeTokenizedCard();
    } catch (err) {
        showError(err.message);
        resetButtonState();
    }
}

async function chargeTokenizedCard() {
    const amount = parseFloat(document.getElementById('payment-amount').value);
    const feeInput = document.getElementById('payment-fee');
    const fee = feeInput ? parseFloat(feeInput.value) : 0;
    const street = document.getElementById('billing-street')?.value?.trim() || '';
    const city = document.getElementById('billing-city')?.value?.trim() || '';
    const stateVal = document.getElementById('billing-state')?.value?.trim() || '';
    const zip = document.getElementById('billing-zip')?.value?.trim() || '';

    if (!amount || isNaN(amount) || amount <= 0) {
        showError('Please enter a valid payment amount.');
        resetButtonState();
        return;
    }

    if (feeInput && !isNaN(fee) && fee < 0) {
        showError('Fee cannot be negative.');
        resetButtonState();
        return;
    }

    const payload = {
        card_token: state.cardToken,
        amount: amount
    };

    if (feeInput && !isNaN(fee) && fee >= 0) {
        payload.fee = fee;
    }

    // Include billing address only if provided by the user
    if (street || city || stateVal || zip) {
        payload.address = {
            street: street,
            city: city,
            state: stateVal,
            zip: zip
        };
    }

    console.log("[chargeTokenizedCard] Initiating charge with payload:", payload);

    try {
        const resp = await fetch('/api/v1/customer/maverick/charge', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload)
        });

        const data = await resp.json().catch(() => ({}));
        if (!resp.ok) {
            const errText = data.error_description || data.message || data.reason || (data.details && data.details.error_code) || data.error || 'Failed to complete payment';
            throw new Error(errText);
        }

        let successMsg = `Payment Successful!\n\nTransaction ID (Inavate): ${data.id}\nInavate Txn Number: ${data.transaction_number}`;
        if (data.reference_number) {
            successMsg += `\nMaverick Transaction ID: ${data.reference_number}`;
        }
        successMsg += `\nStatus: ${data.status}\nBase Amount: $${data.amount.toFixed(2)}`;
        if (data.fee !== undefined && data.fee !== null && data.fee > 0) {
            successMsg += `\nFee: $${data.fee.toFixed(2)}`;
        }
        if (data.total_amount !== undefined && data.total_amount !== null) {
            successMsg += `\nTotal Charged: $${data.total_amount.toFixed(2)}`;
        }
        showSuccess(successMsg);

        // Update custom button to successful paid state
        const btn = document.getElementById('custom-pay-button');
        if (btn) {
            btn.disabled = true;
            btn.innerHTML = 'Paid';
        }
    } catch (err) {
        showError('Payment failed: ' + err.message);
        resetButtonState();
    }
}

function resetButtonState() {
    const btn = document.getElementById('custom-pay-button');
    if (btn) {
        btn.disabled = false;
        btn.innerHTML = 'Pay Now';
    }
    const submitWrapper = document.getElementById('submit-button');
    if (submitWrapper) {
        submitWrapper.style.pointerEvents = 'auto';
    }
}

function showError(msg) {
    const el = document.getElementById('error-display');
    if (msg) {
        el.innerText = msg;
        el.style.display = 'block';
    } else {
        el.style.display = 'none';
    }
}

function showSuccess(msg) {
    const el = document.getElementById('success-display');
    if (msg) {
        el.innerText = msg;
        el.style.display = 'block';
    } else {
        el.style.display = 'none';
    }
}

function resetForm() {
    state.cardToken = '';
    state.maverickToken = '';

    // Reset custom button
    resetButtonState();

    // Clear iframe container
    document.getElementById('submit-button').innerHTML = '';
    initializeHostedFields();
}
