const BLOCKS_UPDATE_INTERVAL = 5 * 1000

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

    setInterval(updateBlocks, BLOCKS_UPDATE_INTERVAL);
});

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

function appendBlock(block) {
    var hash = $('<a>').html(block.Hash).attr('href', '#' + block.Hash).attr('uk-toggle', '');
    var title = $('<span>').html('Block hash: ').append(hash);

    var body = $('<ul>').addClass('uk-list');
    body.append($('<li>').html('Timestamp: ' + block.Timestamp).addClass('uk-text-meta'));
    body.append($('<li>').html('Height: ' + block.Height).addClass('uk-text-meta'));
    body.append($('<li>').html('Nonce: ' + block.Nonce).addClass('uk-text-meta'));
    body.append($('<li>').html('Previous block: ' + block.PrevHash).addClass('uk-text-meta'));
    body.append($('<li>').html('# of tx: ' + block.Txs.length).addClass('uk-text-meta'));

    var elem = $('<li>');
    elem.append(title);
    elem.append(body);
    $('#blocks-list').append(elem);

    if($('#' + block.Hash).length == 0) {
        // Append canvas if does not exists yet
        var txDetails = $('<ul>').addClass('uk-list uk-list-divider');
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

        var offcanvasBar = $('<div>').addClass('uk-offcanvas-bar');
        offcanvasBar.append($('<button>').addClass('uk-offcanvas-close').attr('uk-close', '').attr('type', 'button'));
        offcanvasBar.append($('<h5>').html('Block transactions'));
        offcanvasBar.append(txDetails);
    
        var offcanvas = $('<div>').attr('id', block.Hash).attr('uk-offcanvas', '').attr('flip', 'true');
        offcanvas.append(offcanvasBar);

        $('#offcanvas-container').append(offcanvas);
    }
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
		UIkit.notification('Transaction added to the pool 🎉', {status: 'success', pos: 'top-right'});
	});
}


function validateTxInput() {
    return ($('#to-input').val() != "" 
            && $('#value-input').val() != "" 
            && !isNaN(parseInt($('#value-input').val(), 10)))
}