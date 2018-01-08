const ADDRESS_UPDATE_INTERVAL = 5 * 1000
const BLOCKS_UPDATE_INTERVAL = 5 * 1000
const BALANCE_UPDATE_INTERVAL = 6 * 1000
const MINER_UPDATE_INTERVAL = 4 * 1000
const MAX_PREFIX_SIZE = 5

var blockchain = {}

$(document).ready(() => {
    $('#tx-form').submit(e => {
        e.preventDefault();
        if (validateTxInput()) {
			submitTx();
		} else {
			window.alert('Fields should be non-empty and the value an integer');
		}
    });

    $('#create-miner').click(e => {
        console.log(e);
        e.preventDefault();
        if (validatePrefixAddr()) {
            createTibcoinNode(true);
        } else {
            window.alert("Cannot have a prefix longer than " + MAX_PREFIX_SIZE + ", should contain only alpha-numeric characters and cannot have a prefix containing '0', 'O', 'I' nor 'l'.");
        }

        return false;
    });

    $('#create-node').click(e => {
        console.log(e);
        e.preventDefault();
        if (validatePrefixAddr()) {
            createTibcoinNode(false);
        } else {
            window.alert("Cannot have a prefix longer than " + MAX_PREFIX_SIZE + ", should contain only alpha-numeric characters and cannot have a prefix containing '0', 'O', 'I' nor 'l'.");
        }

        return false;
    });

    $('#mining-form').submit(e => {
        e.preventDefault();
        switchMiningStatus();
    });

    $('#switch-mining-div').hide();

    updateBlocks();
    getAddress();
    updateBalance();
    updateMinerStatus();
    setInterval(getAddress, ADDRESS_UPDATE_INTERVAL);
    setInterval(updateMinerStatus, MINER_UPDATE_INTERVAL);
    setInterval(updateBlocks, BLOCKS_UPDATE_INTERVAL);
    setInterval(updateBalance, BALANCE_UPDATE_INTERVAL);
});

function createTibcoinNode(miner) {
    if (miner) {
        url = "/miner-tibcoin-node";
    }
    else {
        url = "/tibcoin-node";
    }

    $.ajax({
        type: "POST",
        url: url,
        data: $("#new-node-form").serialize(),
    })
    .fail(function() {
        UIkit.notification('Something went wrong when trying to create the node (node already created?), check the logs.', {status: 'danger', pos: 'top-right'});
    })
    .done(function() {
        $('#prefix-input').val("");
        $('#create-node-div').hide();
        UIkit.notification('Your address is being generated...', {status: 'success', pos: 'top-right'});
    });
}

function switchMiningStatus() {
    $.ajax({
        type: "POST",
        url: "/switch-mining-status",
    })
    .fail(function() {
        UIkit.notification('Something went wrong when trying to switch the mining status.', {status: 'danger', pos: 'top-right'});
    })
    .done(function() {
        updateMinerStatus();
        UIkit.notification('Switched mining status.', {status: 'success', pos: 'top-right'});
    });
}

function getAddress() {
    $.ajax({
		type: "GET",
		url: "/address",
    })
    .fail(function() {
        console.log('Could not update address')
    })
    .done(function(data) {
        if (JSON.parse(data)["address"] === undefined || JSON.parse(data)["address"].length == 0) {
            $('#address').html("[Not generated yet]");
        }
        else {
            $('#address').html(JSON.parse(data)["address"]);
            $('#create-node-div').hide();
            $('#switch-mining-div').show();
            console.log(1);
        }
	});
}

function updateBlocks() {
    $.ajax({
		type: "GET",
		url: "/blockchain",
    })
    .fail(function() {
        console.log('Could not update blockchain')
    })
    .done(function(data) {
        var blocks = JSON.parse(data)["blocks"];
        
        // Insert each block into map and display them
        $('#blocks-list').empty()
        blocks.forEach(block => {
            blockchain[block.Hash] = block.Block
            appendBlock(block)
        });
        
	});
}

function updateMinerStatus() {
    $.ajax({
        type: "GET",
        url: "/miner",
    })
    .fail(function() {
        console.log('Could not know if miner or not');
    })
    .done(function(data) {
        var miner = JSON.parse(data)["miner"];

        console.log(miner);

        if (miner) {
            $("#switch-mining").html("Stop mining");
        }
        else {
            $("#switch-mining").html("Start mining");
        }
    });
}

function appendBlock(block) {
    var hash = $('<a>').html(block.Hash).attr('href', '#' + block.Hash).attr('uk-toggle', '');
    var title = $('<span>').html('Block hash: ').append(hash);

    var body = $('<ul>').addClass('uk-list');
    body.append($('<li>').html('Timestamp: ' + block.Timestamp).addClass('uk-text-meta'));
    body.append($('<li>').html('Height: ' + block.Height).addClass('uk-text-meta'));
    body.append($('<li>').html('Nonce: ' + block.Nonce).addClass('uk-text-meta'));
    body.append($('<li>').html('Previous block: ' + block.PrevHash).addClass('uk-text-meta'));
    body.append($('<li>').html('Transactions hash: ' + block.TransactionsHash).addClass('uk-text-meta'));
    body.append($('<li>').html('Target: ' + block.Target).addClass('uk-text-meta'));

    var txNb = 0
    if (block.Txs != null) {
        txNb = block.Txs.length
    }
    body.append($('<li>').html('# of tx: ' + txNb).addClass('uk-text-meta'));

    var elem = $('<li>');
    elem.append(title);
    elem.append(body);
    $('#blocks-list').append(elem);

    if($('#' + block.Hash).length == 0) {
        // Append canvas if does not exists yet
        var txDetails = $('<ul>').addClass('uk-list uk-list-divider');
        if (block.Txs != null) {
            block.Txs.forEach(tx => {
                var txContent = $('<li>');
                txContent.append($('<div>').html('<span class="uk-text-success">From:</span> ' + tx.Address).addClass('uk-text-meta'));
                txContent.append($('<div>').html('<span class="uk-text-success">Hash:</span> ' + tx.Hash).addClass('uk-text-meta'));
                txContent.append($('<div>').html('Outputs').addClass('uk-text-meta uk-text-success'));
                var outputs = $('<ul>').addClass('uk-list');
                tx.Tx.Outputs.forEach(output => {
                    outputs.append($('<li>').html(output.Value + ' tibcoins to ' + output.To).addClass('uk-text-meta'));
                });
    
                txContent.append(outputs);
                txDetails.append(txContent);
            });
        }

        var modelBody = $('<div>').addClass('uk-modal-dialog uk-modal-body');
        modelBody.append($('<h5>').html('Block transactions'));
        modelBody.append($('<div>').html('Block hash: ' + block.Hash).addClass('uk-text-meta'));
        modelBody.append(txDetails);
    
        var modal = $('<div>').attr('id', block.Hash).attr('uk-modal', '');
        modal.append(modelBody);

        $('#modals-container').append(modal);
    }
}

function updateBalance() {
    $.ajax({
		type: "GET",
		url: "/balance",
    })
    .fail(function() {
        console.log('Could not update balance')
    })
    .done(function(data) {
        $('#balance').html(JSON.parse(data)["balance"]);
	});
}

function submitTx() {
    $.ajax({
		type: "POST",
		url: "/tx",
		data: $("#tx-form").serialize(),
    })
    .fail(function() {
        UIkit.notification('It seems you don\'t have enough money ☹️', {status: 'danger', pos: 'top-right'});
    })
    .done(function() {
        $('#to-input').val("");
        $('#value-input').val("");
		UIkit.notification('Tx added to the pool 🎉', {status: 'success', pos: 'top-right'});
	});
}

function validateTxInput() {
    return ($('#to-input').val() != "" 
            && $('#value-input').val() != "" 
            && !isNaN(parseInt($('#value-input').val(), 10)))
}

function validatePrefixAddr() {
    prefix = $('#prefix-input').val();
    return prefix.length == 0 || (prefix.length <= MAX_PREFIX_SIZE && /^[a-zA-Z0-9]+$/.test(prefix) && prefix.indexOf('0') === -1 && prefix.indexOf('O') === -1 && prefix.indexOf('I') === -1 && prefix.indexOf('l') === -1);
}
