$(document).ready(() => {
	$('#name-form').submit(e => submitInput(e, "/name", "#name-form", "#name"));
	$('#peer-form').submit(e => submitInput(e, "/peer", "#peer-form", "#peer"));
});
