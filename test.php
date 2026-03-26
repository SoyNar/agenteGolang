<?php
function getUser($id) {
    $db = new mysqli("localhost", "root", "", "app");
    $q = "SELECT * FROM users WHERE id=" . $id;
    return $db->query($q)->fetch_assoc();
}
